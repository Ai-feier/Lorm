package model

import (
	"github.com/Ai-feier/lorm/internal/errs"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

type Option func(m *Model) error

// Registry 元数据注册中心的抽象
type Registry interface {
	// Get 查找元数据
	Get(val any) (*Model, error)
	// Register 注册一个模型
	Register(val any, opts ...Option) (*Model, error)
}

func WithColumnName(field string, columnName string) Option {
	return func(model *Model) error {
		fd, ok := model.FieldMap[field]
		if !ok {
			return errs.NewErrUnknownField(field)
		}
		// 注意，这里我们根本没有检测 ColName 会不会是空字符串
		// 因为正常情况下，用户都不会写错
		// 即便写错了，也很容易在测试中发现
		fd.ColName = columnName
		return nil
	}
}

type registry struct {
	models sync.Map
}

func NewRegistry() Registry {
	return &registry{}
}

func (r *registry) Get(val any) (*Model, error) {
	typ := reflect.TypeOf(val)
	m, ok := r.models.Load(typ)
	if ok {
		return m.(*Model), nil
	}
	return r.Register(val)
}

func (r *registry) Register(val any, opts ...Option) (*Model, error) {
	// 解析 val
	m, err := r.parseModel(val)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		err := opt(m)
		if err != nil {
			return nil, err
		}
	}
	typ := reflect.TypeOf(val)
	r.models.Store(typ, m)
	return m, nil
}

// 接受一级结构体指针
// parseModel 支持从标签中提取自定义设置
// 标签形式 orm:"key1=value1,key2=value2"
func (r *registry) parseModel(val any) (*Model, error) {
	typ := reflect.TypeOf(val)
	if typ.Kind() != reflect.Ptr ||
		typ.Elem().Kind() != reflect.Struct {
		return nil, errs.ErrPointerOnly
	}
	typ = typ.Elem()

	// 获得字段的数量
	numField := typ.NumField()
	fds := make(map[string]*Field, numField)
	fields := make([]*Field, 0, numField)
	colMap := make(map[string]*Field, numField)
	for i := 0; i < numField; i++ {
		fdType := typ.Field(i)
		tags, err := r.parseTag(fdType.Tag)
		if err != nil {
			return nil, err
		}
		colName := tags[tagKeyColumn]
		if colName == "" {
			colName = underscoreName(fdType.Name)
		}
		f := &Field{
			ColName: colName,
			Type:    fdType.Type,
			GoName:  fdType.Name,
			Offset:  fdType.Offset,
			Index:   i,
		}
		fds[fdType.Name] = f
		fields = append(fields, f)
		colMap[colName] = f
	}
	var tableName string
	if tn, ok := val.(TableName); ok {
		tableName = tn.TableName()
	}

	if tableName == "" {
		tableName = underscoreName(typ.Name())
	}

	return &Model{
		TableName: tableName,
		Fields:    fields,
		FieldMap:  fds,
		ColumnMap: colMap,
	}, nil
}

func (r *registry) parseTag(tag reflect.StructTag) (map[string]string, error) {
	ormTag := tag.Get("orm")
	if ormTag == "" {
		return map[string]string{}, nil
	}
	res := make(map[string]string, 1)

	// 切割字符串
	pairs := strings.Split(ormTag, ",")
	for _, pair := range pairs {
		kv := strings.Split(pair, "=")
		if len(kv) != 2 {
			return nil, errs.NewErrInvalidTagContent(pair)
		}
		res[kv[0]] = kv[1]
	}
	return res, nil
}

// underscoreName 驼峰转字符串命名
func underscoreName(tableName string) string {
	var buf []byte
	for i, v := range tableName {
		if unicode.IsUpper(v) {
			if i != 0 && tableName[i-1:i+1] != "ID" {
				buf = append(buf, '_')
			}
			buf = append(buf, byte(unicode.ToLower(v)))
		} else {
			buf = append(buf, byte(v))
		}
	}
	return string(buf)
}

func WithTableName(name string) Option {
	return func(m *Model) error {
		m.TableName = name
		return nil
	}
}

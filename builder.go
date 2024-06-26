package lorm

import (
	"github.com/Ai-feier/lorm/internal/errs"
	"github.com/Ai-feier/lorm/model"
	"strings"
)

type builder struct {
	// 构造 SQL
	sb strings.Builder
	// 存放 SQL 参数
	args []any
	// 存放当前对象的元数据信息
	model *model.Model
	// 方言抽象
	dialect Dialect
	r model.Registry
	quoter  byte
}

// buildColumn 构造列
// 如果 table 没有指定，我们就用 model 来判断列是否存在
func (b *builder) buildColumn(table TableReference, fd string) error {
	var alias string
	if table != nil {
		alias = table.tableAlias()
	}
	if alias != "" {
		b.quote(alias)
		b.sb.WriteByte('.')
	}
	colName, err := b.colName(table, fd)
	if err != nil {
		return err
	}
	b.quote(colName)
	return nil
}

func (b *builder) colName(table TableReference, fd string) (string, error) {
	switch tab := table.(type) {
	case nil:
		fdMeta, ok := b.model.FieldMap[fd]
		if !ok {
			return "", errs.NewErrUnknownField(fd)
		}
		return fdMeta.ColName, nil
	case Table:
		m, err := b.r.Get(tab.entity)
		if err != nil {
			return "", err
		}
		fdMeta, ok := m.FieldMap[fd]
		if !ok {
			return "", errs.NewErrUnknownField(fd)
		}
		return fdMeta.ColName, nil
	case Join:
		colName, err := b.colName(tab.left, fd)
		if err != nil {
			return colName, nil
		}
		return b.colName(tab.right, fd)
	case Subquery:
		if len(tab.columns) > 0 {
			for _, c := range tab.columns {
				if c.selectedAlias() == fd {
					return fd, nil
				}

				if c.fieldName() == fd {
					return b.colName(c.target(), fd)
				}
			}
			return "", errs.NewErrUnknownField(fd)
		}
		return b.colName(tab.table, fd)
	default:
		return "", errs.NewErrUnsupportedExpressionType(tab)
	}
}

// 构造方言的quote
func (b *builder) quote(name string) {
	b.sb.WriteByte(b.quoter)
	b.sb.WriteString(name)
	b.sb.WriteByte(b.quoter)
}

// 构造原生表达式
func (b *builder) raw(r RawExpr) {
	b.sb.WriteString(r.raw)
	if len(r.args) != 0 {
		b.addArgs(r.args...)
	}
}

// 构造 sql 语句参数
func (b *builder) addArgs(args ...any) {
	if b.args == nil {
		// 很少有查询能够超过八个参数
		// INSERT 除外
		b.args = make([]any, 0, 8)
	}
	b.args = append(b.args, args...)
}

// 构造条件表达式
func (b *builder) buildPredicates(ps []Predicate) error {
	p := ps[0]
	for i := 1; i < len(ps); i++ {
		p = p.And(ps[i])
	}
	return b.buildExpression(p)
}

// 构造表达式
func (b *builder) buildExpression(e Expression) error {
	if e == nil {
		return nil
	}
	switch exp := e.(type) {
	case Column:
		return b.buildColumn(exp.table, exp.name)
	case Aggregate:
		return b.buildAggregate(exp, false)
	case value:
		b.sb.WriteByte('?')
		b.addArgs(exp.val)
	case RawExpr:
		b.sb.WriteString(exp.raw)
		if len(exp.args) != 0 {
			b.addArgs(exp.args...)
		}
	case MathExpr:
		return b.buildBinaryExpr(binaryExpr(exp))
	case Predicate:
		return b.buildBinaryExpr(binaryExpr(exp))
	case binaryExpr:
		return b.buildBinaryExpr(exp)
	case SubqueryExpr:
		b.sb.WriteString(exp.pred)
		b.sb.WriteByte(' ')
		return b.buildSubquery(exp.s, false)
	case Subquery:
		return b.buildSubquery(exp, false)
	default:
		return errs.NewErrUnsupportedExpressionType(exp)
	}
	return nil
}

// 构造二分表达式
func (b *builder) buildBinaryExpr(e binaryExpr) error {
	err := b.buildSubExpr(e.left)
	if err != nil {
		return err
	}
	if e.op != "" {
		b.sb.WriteByte(' ')
		b.sb.WriteString(e.op.String())
	}
	if e.right != nil {
		b.sb.WriteByte(' ')
		return b.buildSubExpr(e.right)
	}
	return nil
}

// 构造二分表达式的右半部分
func (b *builder) buildSubExpr(subExpr Expression) error {
	switch sub := subExpr.(type) {
	case MathExpr:
		_ = b.sb.WriteByte('(')
		if err := b.buildBinaryExpr(binaryExpr(sub)); err != nil {
			return err
		}
		_ = b.sb.WriteByte(')')
	case binaryExpr:
		_ = b.sb.WriteByte('(')
		if err := b.buildBinaryExpr(sub); err != nil {
			return err
		}
		_ = b.sb.WriteByte(')')
	case Predicate:
		_ = b.sb.WriteByte('(')
		if err := b.buildBinaryExpr(binaryExpr(sub)); err != nil {
			return err
		}
		_ = b.sb.WriteByte(')')
	default:
		if err := b.buildExpression(sub); err != nil {
			return err
		}
	}
	return nil
}

// 构造 Aggregate 表达式
func (b *builder) buildAggregate(a Aggregate, useAlias bool) error {
	b.sb.WriteString(a.fn)
	b.sb.WriteByte('(')
	err := b.buildColumn(a.table, a.arg)
	if err != nil {
		return err
	}
	b.sb.WriteByte(')')
	if useAlias {
		b.buildAs(a.alias)
	}
	return nil
}

// 构造 As 别名
func (b *builder) buildAs(alias string) {
	if alias != "" {
		b.sb.WriteString(" AS ")
		b.quote(alias)
	}
}


func (b *builder) buildSubquery(tab Subquery, useAlias bool) error {
	q, err := tab.s.Build()
	if err != nil {
		return err
	}
	b.sb.WriteByte('(')
	b.sb.WriteString(q.SQL[:len(q.SQL)-1])
	if len(q.Args) > 0 {
		b.addArgs(q.Args...)
	}
	b.sb.WriteByte(')')
	if useAlias {
		b.sb.WriteString(" AS ")
		b.quote(tab.alias)
	}
	return nil
}


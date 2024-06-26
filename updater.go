package lorm

import (
	"context"
	"github.com/Ai-feier/lorm/internal/errs"
)

type Updater[T any] struct {
	builder
	db      *DB
	assigns []Assignable
	val     *T
	where   []Predicate
}

func NewUpdater[T any](db *DB) *Updater[T] {
	return &Updater[T]{
		builder: builder{
			dialect: db.dialect,
			quoter:  db.dialect.quoter(),
			r: db.r,
		},
		db: db,
	}
}

func (u *Updater[T]) Build() (*Query, error) {
	defer func() {
		u.sb.Reset()
	}()
	if len(u.assigns) == 0 {
		return nil, errs.ErrNoUpdatedColumns
	}
	var err error
	var t T
	u.model, err = u.db.r.Get(&t)
	if err != nil {
		return nil, err
	}
	u.sb.WriteString("UPDATE ")
	u.quote(u.model.TableName)
	u.sb.WriteString(" SET ")
	val := u.db.valCreator(u.val, u.model)
	for i, a := range u.assigns {
		if i > 0 {
			u.sb.WriteByte(',')
		}
		switch assign := a.(type) {
		case Column:
			if err = u.buildColumn(assign.table, assign.name); err != nil {
				return nil, err
			}
			u.sb.WriteString("=?")
			arg, err := val.Field(assign.name)
			if err != nil {
				return nil, err
			}
			u.addArgs(arg)
		case Assignment:
			if err = u.buildAssignment(assign); err != nil {
				return nil, err
			}
		default:
			return nil, errs.NewErrUnsupportedAssignableType(a)
		}
	}
	if len(u.where) > 0 {
		u.sb.WriteString(" WHERE ")
		if err = u.buildPredicates(u.where); err != nil {
			return nil, err
		}
	}
	u.sb.WriteByte(';')
	return &Query{
		SQL:  u.sb.String(),
		Args: u.args,
	}, nil
}

func (u *Updater[T]) Exec(ctx context.Context) Result {
	//q, err := u.Build()
	//if err != nil {
	//	return Result{err: err}
	//}
	//res, err := u.db.execContext(ctx, q.SQL, q.Args...)
	//return Result{res: res, err: err}

	handler := u.execHandler
	mdls := u.db.mdls
	for i:=len(mdls)-1;i>=0;i-- {
		handler = mdls[i](handler)
	}
	qc := &QueryContext{
		Builder: u,
		Type: "DELETE",
		Model: u.model,
	}
	qr := handler(ctx, qc)
	return qr.Result.(Result)
}

func (u *Updater[T]) execHandler(ctx context.Context, qc *QueryContext) *QueryResult {
	q, err := u.Build()
	if err != nil {
		return &QueryResult{
			Result: Result{err: err},
		}
	}
	res, err := u.db.execContext(ctx, q.SQL, q.Args...)
	return &QueryResult{
		Result: Result{res: res, err: err},
	}
}

func (u *Updater[T]) Set(assigns ...Assignable) *Updater[T] {
	u.assigns = assigns
	return u
}

func (u *Updater[T]) Update(t *T) *Updater[T] {
	u.val = t
	return u
}

func (u *Updater[T]) Where(ps ...Predicate) *Updater[T] {
	u.where = ps
	return u
}

func (u *Updater[T]) buildAssignment(assign Assignment) error {
	if err := u.buildColumn(nil, assign.column); err != nil {
		return err
	}
	u.sb.WriteByte('=')
	return u.buildExpression(assign.val)
}

package webui

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/pogo-vcs/pogo/auth"
	"github.com/pogo-vcs/pogo/db"
)

func GetParam(ctx context.Context, name string) (string, bool) {
	v := ctx.Value(name)
	if v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func GetParamI32(ctx context.Context, name string) (int32, bool) {
	v, ok := GetParam(ctx, name)
	if !ok {
		return 0, false
	}
	i, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(i), true
}

type UiContext struct {
	ctx  context.Context
	req  *http.Request
	user *db.User
}

func NewUiContext(req *http.Request) context.Context {
	ctx := &UiContext{
		ctx: req.Context(),
		req: req,
	}

	if userVal := req.Context().Value(auth.UserCtxKey); userVal != nil {
		if user, ok := userVal.(*db.User); ok {
			ctx.user = user
		}
	}

	return ctx
}

func (c *UiContext) User() *db.User {
	return c.user
}

func (c *UiContext) Deadline() (deadline time.Time, ok bool) {
	return c.ctx.Deadline()
}

func (c *UiContext) Done() <-chan struct{} {
	return c.ctx.Done()
}

func (c *UiContext) Err() error {
	return c.ctx.Err()
}

func (c *UiContext) Value(key any) any {
	switch key := key.(type) {
	case string:
		switch key {
		case auth.UserCtxKey:
			return c.user
		default:
			if v := c.req.PathValue(key); v != "" {
				return v
			}
		}
	}
	return c.ctx.Value(key)
}

func GetUser(ctx context.Context) *db.User {
	up := ctx.Value(auth.UserCtxKey)
	if up == nil {
		return nil
	}
	if user, ok := up.(*db.User); ok {
		return user
	}
	return nil
}

func IsLoggedIn(ctx context.Context) bool {
	up := ctx.Value(auth.UserCtxKey)
	if up == nil {
		return false
	}
	if user, ok := up.(*db.User); ok {
		return user != nil
	}
	return false
}

package foo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// The functions below stand in for the client generated from the OpenAPI
// document. A real resource calls that client with c.Server and c.Token
// instead. They behave like a server would: they refuse a request without a
// token, assign the ID and the creation time, keep names unique, and merge a
// patch. Their errors are plain, because a server's message is shown as
// received.

const fileName = "foo.json"

func storePath() (string, error) {
	dir, err := profile.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

func load() ([]*Foo, error) {
	p, err := storePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []*Foo
	return items, json.Unmarshal(b, &items)
}

func save(items []*Foo) error {
	p, err := storePath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return profile.WriteFileAtomic(p, b)
}

// authorize is the server's token check.
func authorize(ctx context.Context, c resource.Conn) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Token == "" {
		return resource.Unauthorized()
	}
	return nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func index(items []*Foo, id string) int {
	return slices.IndexFunc(items, func(f *Foo) bool { return str(f.ID) == id })
}

// nameTaken reports whether another object has the name.
func nameTaken(items []*Foo, name, exceptID string) bool {
	return slices.ContainsFunc(items, func(f *Foo) bool {
		return str(f.Name) == name && str(f.ID) != exceptID
	})
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "foo-" + hex.EncodeToString(b)
}

// clone copies by JSON, which also copies the pointers.
func clone(f *Foo) *Foo {
	b, _ := json.Marshal(f)
	var out Foo
	_ = json.Unmarshal(b, &out)
	return &out
}

func get(ctx context.Context, c resource.Conn, id string) (*Foo, error) {
	if err := authorize(ctx, c); err != nil {
		return nil, err
	}
	items, err := load()
	if err != nil {
		return nil, err
	}
	i := index(items, id)
	if i < 0 {
		return nil, resource.NotFound("foo", id)
	}
	return clone(items[i]), nil
}

// list filters by a name that contains the search text. Limit 0 returns all.
// Page starts at 1.
func list(ctx context.Context, c resource.Conn, q resource.ListQuery) ([]*Foo, error) {
	if err := authorize(ctx, c); err != nil {
		return nil, err
	}
	items, err := load()
	if err != nil {
		return nil, err
	}
	var out []*Foo
	for _, f := range items {
		if q.Search == "" || strings.Contains(str(f.Name), q.Search) {
			out = append(out, clone(f))
		}
	}
	slices.SortFunc(out, func(a, b *Foo) int { return strings.Compare(str(a.Name), str(b.Name)) })

	if q.Limit > 0 {
		start := max(q.Page-1, 0) * q.Limit
		if start > len(out) {
			start = len(out)
		}
		out = out[start:min(start+q.Limit, len(out))]
	}
	return out, nil
}

func create(ctx context.Context, c resource.Conn, v *Foo) (*Foo, error) {
	if err := authorize(ctx, c); err != nil {
		return nil, err
	}
	if str(v.Name) == "" {
		return nil, errors.New("name is required")
	}
	items, err := load()
	if err != nil {
		return nil, err
	}
	if nameTaken(items, str(v.Name), "") {
		return nil, fmt.Errorf("a foo named %q already exists", str(v.Name))
	}

	f := clone(v)
	id, now := newID(), time.Now().UTC().Truncate(time.Second)
	f.ID, f.CreatedAt = &id, &now
	if err := save(append(items, f)); err != nil {
		return nil, err
	}
	return clone(f), nil
}

// replace is a full update: whatever the request leaves out is cleared. Only
// the ID and the creation time are kept.
func replace(ctx context.Context, c resource.Conn, id string, v *Foo) (*Foo, error) {
	if err := authorize(ctx, c); err != nil {
		return nil, err
	}
	if str(v.Name) == "" {
		return nil, errors.New("name is required")
	}
	items, err := load()
	if err != nil {
		return nil, err
	}
	i := index(items, id)
	if i < 0 {
		return nil, resource.NotFound("foo", id)
	}
	if nameTaken(items, str(v.Name), id) {
		return nil, fmt.Errorf("a foo named %q already exists", str(v.Name))
	}

	f := clone(v)
	f.ID, f.CreatedAt = items[i].ID, items[i].CreatedAt
	items[i] = f
	if err := save(items); err != nil {
		return nil, err
	}
	return clone(f), nil
}

// patch is a partial update: only what the request carries changes. A nested
// object is merged property by property. A list is replaced as a whole.
func patch(ctx context.Context, c resource.Conn, id string, v *Foo) (*Foo, error) {
	if err := authorize(ctx, c); err != nil {
		return nil, err
	}
	items, err := load()
	if err != nil {
		return nil, err
	}
	i := index(items, id)
	if i < 0 {
		return nil, resource.NotFound("foo", id)
	}
	f := items[i]

	if v.Name != nil {
		if *v.Name == "" {
			return nil, errors.New("name cannot be empty")
		}
		if nameTaken(items, *v.Name, id) {
			return nil, fmt.Errorf("a foo named %q already exists", *v.Name)
		}
		f.Name = v.Name
	}
	if v.Description != nil {
		f.Description = v.Description
	}
	if v.Enabled != nil {
		f.Enabled = v.Enabled
	}
	if v.Tags != nil {
		f.Tags = v.Tags
	}
	if v.Secret != nil {
		f.Secret = v.Secret
	}
	if v.Settings != nil {
		if f.Settings == nil {
			f.Settings = &FooSettings{}
		}
		if v.Settings.Timeout != nil {
			f.Settings.Timeout = v.Settings.Timeout
		}
		if v.Settings.Retries != nil {
			f.Settings.Retries = v.Settings.Retries
		}
	}

	if err := save(items); err != nil {
		return nil, err
	}
	return clone(f), nil
}

func remove(ctx context.Context, c resource.Conn, id string) error {
	if err := authorize(ctx, c); err != nil {
		return err
	}
	items, err := load()
	if err != nil {
		return err
	}
	i := index(items, id)
	if i < 0 {
		return resource.NotFound("foo", id)
	}
	return save(slices.Delete(items, i, i+1))
}

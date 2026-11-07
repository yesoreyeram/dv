package frame

import (
	"errors"
	"fmt"
)

// ErrLimitReached is returned by Builder.AppendRow when a row or byte limit stops the
// build. The frame keeps the rows added before the limit.
var ErrLimitReached = errors.New("frame limit reached")

// Limits bound the size of a frame while it is built (NFR-PERF-003). Zero means
// unlimited.
type Limits struct {
	MaxRows  int
	MaxBytes int64
}

// Builder appends rows to a frame while enforcing Limits.
type Builder struct {
	frame  *Frame
	limits Limits
	bytes  int64
	done   bool
}

// NewBuilder returns a builder for an empty frame with the given fields.
func NewBuilder(name string, limits Limits, fields ...*Field) (*Builder, error) {
	if limits.MaxRows < 0 || limits.MaxBytes < 0 {
		return nil, errors.New("limits must not be negative")
	}
	for _, f := range fields {
		if f != nil && f.Len() != 0 {
			return nil, fmt.Errorf("field %q must be empty", f.Name)
		}
	}
	fr, err := New(name, fields...)
	if err != nil {
		return nil, err
	}
	return &Builder{frame: fr, limits: limits}, nil
}

// AppendRow adds a row unless a limit would be exceeded, in which case the frame is
// marked truncated, a warning notice is added, and ErrLimitReached is returned. Further
// calls keep returning ErrLimitReached. Type errors are returned without truncating.
func (b *Builder) AppendRow(vals ...any) error {
	if b.done {
		return ErrLimitReached
	}
	if b.limits.MaxRows > 0 && b.frame.Rows() >= b.limits.MaxRows {
		return b.stop(fmt.Sprintf("Result truncated at the row limit of %d rows.", b.limits.MaxRows))
	}
	var size int64
	for _, v := range vals {
		size += int64(estimateSize(v))
	}
	if b.limits.MaxBytes > 0 && b.bytes+size > b.limits.MaxBytes {
		return b.stop(fmt.Sprintf("Result truncated at the byte limit of %d bytes.", b.limits.MaxBytes))
	}
	if err := b.frame.AppendRow(vals...); err != nil {
		return err
	}
	b.bytes += size
	return nil
}

func (b *Builder) stop(msg string) error {
	b.done = true
	b.frame.Meta.Truncated = true
	b.frame.AddNotice(SeverityWarning, msg)
	return ErrLimitReached
}

// Bytes returns the estimated size of the rows added so far.
func (b *Builder) Bytes() int64 { return b.bytes }

// Frame returns the frame built so far.
func (b *Builder) Frame() *Frame { return b.frame }

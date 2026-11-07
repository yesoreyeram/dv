package frame

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Wire format (ADR-0003, FR-010 AC4):
//
//	{"schema":{"name":"cpu","meta":{…},"fields":[{"name":"time","type":"time","config":{}}]},
//	 "data":{"values":[[1794045600000, …], …]}}
//
// Values are column-major; times are UTC epoch milliseconds; null is JSON null.

type wireFrame struct {
	Schema wireSchema `json:"schema"`
	Data   wireData   `json:"data"`
}

type wireSchema struct {
	Name   string      `json:"name"`
	Meta   *Meta       `json:"meta,omitempty"`
	Fields []wireField `json:"fields"`
}

type wireField struct {
	Name   string      `json:"name"`
	Type   FieldType   `json:"type"`
	Config FieldConfig `json:"config"`
}

type wireData struct {
	Values []json.RawMessage `json:"values"`
}

var jsonNull = []byte("null")

// MarshalJSON encodes the frame in the column-major wire format.
func (f *Frame) MarshalJSON() ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	w := wireFrame{
		Schema: wireSchema{Name: f.Name, Fields: make([]wireField, len(f.Fields))},
		Data:   wireData{Values: make([]json.RawMessage, len(f.Fields))},
	}
	if !isZeroMeta(f.Meta) {
		m := f.Meta
		w.Schema.Meta = &m
	}
	for i, fld := range f.Fields {
		w.Schema.Fields[i] = wireField{Name: fld.Name, Type: fld.Type, Config: fld.Config}
		col, err := encodeColumn(fld)
		if err != nil {
			return nil, err
		}
		w.Data.Values[i] = col
	}
	return json.Marshal(w)
}

func isZeroMeta(m Meta) bool {
	return m.Source == "" && m.ExecutedQuery == "" && !m.Truncated && len(m.Notices) == 0
}

func encodeColumn(fld *Field) (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, v := range fld.values {
		if i > 0 {
			buf.WriteByte(',')
		}
		switch x := v.(type) {
		case nil:
			buf.Write(jsonNull)
		case time.Time:
			buf.WriteString(strconv.FormatInt(x.UnixMilli(), 10))
		case int64:
			buf.WriteString(strconv.FormatInt(x, 10))
		case float64:
			// Keep a fractional marker so integral floats decode back as float64.
			s := strconv.FormatFloat(x, 'g', -1, 64)
			if !strings.ContainsAny(s, ".eE") {
				s += ".0"
			}
			buf.WriteString(s)
		case json.RawMessage:
			buf.Write(x)
		default: // string, bool
			b, err := json.Marshal(x)
			if err != nil {
				return nil, fmt.Errorf("field %q row %d: %w", fld.Name, i, err)
			}
			buf.Write(b)
		}
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// wireFrameIn mirrors wireFrame with pointers so absent or null containers can be told
// apart from valid empty ones while decoding.
type wireFrameIn struct {
	Schema *struct {
		Name   string      `json:"name"`
		Meta   *Meta       `json:"meta,omitempty"`
		Fields []wireField `json:"fields"`
	} `json:"schema"`
	Data *struct {
		Values []json.RawMessage `json:"values"`
	} `json:"data"`
}

// UnmarshalJSON decodes the wire format, validating field types and column lengths.
// Unknown members, and absent or null schema/data/fields/values containers, are
// rejected.
func (f *Frame) UnmarshalJSON(b []byte) error {
	var in wireFrameIn
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("decode frame: %w", err)
	}
	switch {
	case in.Schema == nil || in.Data == nil:
		return errors.New("decode frame: schema and data objects are required")
	case in.Schema.Fields == nil:
		return errors.New("decode frame: schema.fields must be an array")
	case in.Data.Values == nil:
		return errors.New("decode frame: data.values must be an array")
	}
	w := wireFrame{
		Schema: wireSchema{Name: in.Schema.Name, Meta: in.Schema.Meta, Fields: in.Schema.Fields},
		Data:   wireData{Values: in.Data.Values},
	}
	if len(w.Data.Values) != len(w.Schema.Fields) {
		return fmt.Errorf("schema has %d fields but data has %d columns: %w",
			len(w.Schema.Fields), len(w.Data.Values), ErrLength)
	}
	fields := make([]*Field, len(w.Schema.Fields))
	for i, wf := range w.Schema.Fields {
		fld, err := NewField(wf.Name, wf.Type)
		if err != nil {
			return err
		}
		fld.Config = wf.Config
		if err := decodeColumn(fld, w.Data.Values[i]); err != nil {
			return err
		}
		fields[i] = fld
	}
	out := Frame{Name: w.Schema.Name, Fields: fields}
	if w.Schema.Meta != nil {
		out.Meta = *w.Schema.Meta
	}
	if err := out.Validate(); err != nil {
		return err
	}
	*f = out
	return nil
}

func decodeColumn(fld *Field, raw json.RawMessage) error {
	var cells []json.RawMessage
	if err := json.Unmarshal(raw, &cells); err != nil {
		return fmt.Errorf("field %q: values must be an array: %w", fld.Name, err)
	}
	if cells == nil {
		return fmt.Errorf("field %q: values must be an array, not null", fld.Name)
	}
	fld.values = make([]any, 0, len(cells))
	for i, c := range cells {
		v, err := decodeCell(fld.Type, c)
		if err != nil {
			return fmt.Errorf("field %q row %d: %w", fld.Name, i, err)
		}
		fld.values = append(fld.values, v)
	}
	return nil
}

var errCell = errors.New("value does not match field type")

func decodeCell(typ FieldType, c json.RawMessage) (any, error) {
	if bytes.Equal(bytes.TrimSpace(c), jsonNull) {
		return nil, nil
	}
	switch typ {
	case TypeTime:
		var ms int64
		if err := json.Unmarshal(c, &ms); err != nil {
			return nil, fmt.Errorf("time must be integer epoch milliseconds: %w", errCell)
		}
		return time.UnixMilli(ms).UTC(), nil
	case TypeNumber:
		if t := bytes.TrimSpace(c); len(t) == 0 || t[0] == '"' {
			return nil, errCell
		}
		var n json.Number
		dec := json.NewDecoder(bytes.NewReader(c))
		dec.UseNumber()
		if err := dec.Decode(&n); err != nil {
			return nil, errCell
		}
		if i, err := n.Int64(); err == nil {
			return i, nil
		}
		fv, err := n.Float64()
		if err != nil || math.IsInf(fv, 0) {
			return nil, errCell
		}
		return fv, nil
	case TypeString:
		var s string
		if err := json.Unmarshal(c, &s); err != nil {
			return nil, errCell
		}
		return s, nil
	case TypeBool:
		var bv bool
		if err := json.Unmarshal(c, &bv); err != nil {
			return nil, errCell
		}
		return bv, nil
	default: // TypeJSON; NewField already rejected unknown types
		var compact bytes.Buffer
		if err := json.Compact(&compact, c); err != nil {
			return nil, errCell
		}
		return json.RawMessage(compact.Bytes()), nil
	}
}

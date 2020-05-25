package nbt

//go:generate msgp

import (
	"io"
)

type TagID byte

const (
	TagEnd = TagID(iota)
	TagByte
	TagShort
	TagInt
	TagLong
	TagFloat
	TagDouble
	TagByteArray
	TagString
	TagList
	TagCompound
	TagIntArray
	TagLongArray
)

type BasicTag struct {
	Header TagHeader
	Value  []byte
}

type TagHeader struct {
	TagID TagID
	Name  []byte
}

func (t *TagHeader) Length() int {
	return 3 + len(t.Name)
}

func (t *BasicTag) Bytes() []byte {
	return append(t.Header.Bytes(), t.Value...)
}

func NewStringTag(name string, body string) *BasicTag {
	value := make([]byte, 2+len(body))
	value[0], value[1] = byte((len(body)>>8)&0xff), byte(len(body)&0xff)
	copy(value[2:], []byte(body))

	return &BasicTag{
		Header: TagHeader{
			TagID: TagString,
			Name:  []byte(name),
		},
		Value: value,
	}
}

func NewIntTag(name string, num int) *BasicTag {
	body := []byte{byte((num >> 24) & 0xff), byte((num >> 16) & 0xff),
		byte((num >> 8) & 0xff), byte((num) & 0xff)}

	return &BasicTag{
		Header: TagHeader{
			TagID: TagInt,
			Name:  []byte(name),
		},
		Value: body,
	}
}

func NewLongTag(name string, num int64) *BasicTag {
	body := []byte{
		byte((num >> 56) & 0xff), byte((num >> 48) & 0xff), byte((num >> 40) & 0xff), byte((num >> 32) & 0xff),
		byte((num >> 24) & 0xff), byte((num >> 16) & 0xff), byte((num >> 8) & 0xff), byte((num) & 0xff),
	}

	return &BasicTag{
		Header: TagHeader{
			TagID: TagLong,
			Name:  []byte(name),
		},
		Value: body,
	}
}

func (t *TagHeader) Bytes() []byte {
	result := make([]byte, 1+2+len(t.Name))
	result[0], result[1], result[2] = byte(t.TagID),
		byte((len(t.Name)>>8)&0xff), byte(len(t.Name)&0xff)
	copy(result[3:], []byte(t.Name))

	return result
}

func (r *Reader) ReadTagHeader() (tagHeader TagHeader, unreadLength int,
	err error) {

	if r.cursor >= len(r.data) {
		err = io.EOF
		return
	}

	tagHeader.TagID = TagID(r.data[r.cursor])
	if tagHeader.TagID == TagEnd {
		r.cursor++
		unreadLength = 1
		return
	}

	length := int(r.data[r.cursor+1])<<8 | int(r.data[r.cursor+2])
	tagHeader.Name = r.data[r.cursor+3 : r.cursor+3+length]

	unreadLength = 3 + length
	r.cursor += unreadLength

	return
}

func (r *Reader) SkipTagHeader() (int, error) {
	if r.cursor >= len(r.data) {
		return 0, io.EOF
	}

	if TagID(r.data[r.cursor]) == TagEnd {
		r.cursor++
		return 1, nil
	}

	unread := 3 + (int(r.data[r.cursor+1])<<8 | int(r.data[r.cursor+2]))
	r.cursor += unread

	return unread, nil
}

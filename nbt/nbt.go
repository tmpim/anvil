package nbt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"reflect"

	"github.com/klauspost/compress/gzip"
	"github.com/tmpim/anvil"
)

var (
	ErrEndOfCompound         = errors.New("nbt: end of compound")
	ErrInvalidType           = errors.New("nbt: invalid type, type must be:")
	ErrInvalidHeaderLocation = errors.New("nbt: invalid header location")
	ErrIndexCorrupt          = errors.New("nbt: index corrupt, report this to 1lann")
	ErrNotIndexed            = errors.New("nbt: indexing is required before calling this method")
)

type Reader struct {
	data   []byte
	cursor int
}

func NewGzipReader(rd io.Reader) (Reader, error) {
	rd, err := gzip.NewReader(rd)
	if err != nil {
		return Reader{}, err
	}
	data, err := ioutil.ReadAll(rd)
	if err != nil {
		return Reader{}, err
	}

	return Reader{
		data:   data,
		cursor: 0,
	}, nil
}

func NewRegionChunkReader(c *anvil.ChunkData) (Reader, error) {
	data, err := c.Decompress()
	if err != nil {
		return Reader{}, err
	}

	return NewReader(data), nil
}

// NewReader creates a new NBT reader. We use raw byte arrays for performance
// as we intend to use this tool to query through gigabytes of data.
// Feel free to use memory mapped files for the performance boost!
func NewReader(data []byte) Reader {
	return Reader{
		data:   data,
		cursor: 0,
	}
}

func (r *Reader) Len() int {
	return len(r.data)
}

func (r Reader) Copy(cursor int) Reader {
	r.cursor = cursor
	return r
}

func (r *Reader) Unread(numBytes int) {
	if numBytes < 0 {
		r.cursor = 0
		return
	} else if numBytes > r.cursor {
		panic("anvil/nbt: cannot unread before start of data")
	}

	r.cursor -= numBytes
}

// Cursor returns the reader's current cursor position. You shouldn't be
// using this unless you know what you're doing.
func (r *Reader) Cursor() int {
	return r.cursor
}

// SeekTo seeks the reader to the specified absolute cursor position.
// You shouldn't be using this unless you know what you're doing.
func (r *Reader) SeekTo(pos int) {
	r.cursor = pos
}

// AlignToIndex seeks up until the cursor is aligned to a valid index entry.
// Returns nil if there is no index, or if it hits the start of the chunk data
// without finding any valid index entries.
// func (r *Reader) AlignToIndex() *IndexEntry {
// 	if r.Index == nil {
// 		return nil
// 	}

// 	for i := r.cursor; i >= 0; i-- {
// 		if ent, found := r.Index[i]; found {
// 			r.SeekTo(i)
// 			return ent
// 		}
// 	}

// 	return nil
// }

// SeekToAndRead seeks to the given name and a tag ID matching the type of `value`
// and reads it into `value`. SeekToAndRead will stop if it reaches the end of
// the current compound, it will leave the cursor pointing to the next header after TagEnd
// and it will return ErrEndOfCompound. If the end of the NBT is reached, io.EOF is returned.
// SeekToAndRead will not recursively
// search through compounds, use SeekToMatchingCompound and ReadCompound for that.
//
// Returns the number of bytes to unread to restore the reader's state
// back to before SeekToAndRead was called if the tag with a name and a tag ID
// matching `value` could not be found.
//
// If the tag could be found with the matching name and value, it will return
// the number of bytes to unread back to the header of the matching tag.
// func (r *Reader) SeekToAndRead(name string, value interface{}) (int, error) {

// }

// RecurSeekToMatchingCompound will search recursively for the first compound
// whose keys match the provided matcher. The names correlate to tag names in order
// of parameters of the given matcher function, the input types of the matcher
// function determines the type those tags will be decoded as and provided
// to the matcher function.
// If the matcher function returns true, the cursor will stop at the first header
// inside the matching compound and will return a stack of readers that represent
// the start of the stack of compounds found, pointing to the header of the compound, where
// readers[0] is the reader closest to the known root, and 0 is returned.
// If no match is ever found, the number of bytes to unread back to the to go back
// to the state before SeekToMatchingCompound was called is returned as the int and the
// reader will be left in an EOF state.
// func (r *Reader) RecurSeekToMatchingCompound(names []string, matcher interface{}) ([]Breadcrumb, int) {

// }

func (r *Reader) VerifyTagHeader() error {
	if r.Index == nil {
		return ErrNotIndexed
	}

	if _, found := r.Index[r.cursor]; !found {
		return ErrInvalidHeaderLocation
	}

	return nil
}

func (r *Reader) readCompound(value interface{}) (int, error) {
	rv := reflect.ValueOf(value)

	underlying := rv.Elem()
	totalUnread := 0

	switch underlying.Kind() {
	case reflect.Struct:
		typ := underlying.Type()
		resultMap := make(map[string]interface{})
		numFields := underlying.NumField()
		for i := 0; i < numFields; i++ {
			fieldType := typ.Field(i)
			tag := fieldType.Tag.Get("nbt")
			if tag == "-" {
				continue
			} else if tag == "" {
				tag = fieldType.Name
			}

			resultMap[tag] = underlying.Field(i).Addr().Interface()
		}

		for {
			header, unread, err := r.ReadTagHeader()
			totalUnread += unread
			if err != nil {
				return totalUnread, err
			}

			if header.TagID == TagEnd {
				break
			}

			val, ok := resultMap[string(header.Name)]
			if !ok {
				continue
			}

			unread, err = r.ReadImmediate(header.TagID, val)
			totalUnread += unread
			if err != nil {
				return totalUnread, err
			}

			underlying.SetMapIndex(reflect.ValueOf(string(header.Name)), reflect.ValueOf(val).Elem())
		}

		return totalUnread, nil
	case reflect.Map:
		if underlying.Type().Key().Kind() != reflect.String {
			return 0, fmt.Errorf("%w map key must be a pointer")
		}

		for {
			header, unread, err := r.ReadTagHeader()
			totalUnread += unread
			if err != nil {
				return totalUnread, err
			}

			if header.TagID == TagEnd {
				break
			}

			result := reflect.New(underlying.Type().Elem())
			unread, err = r.ReadImmediate(header.TagID, result.Interface())
			totalUnread += unread
			if err != nil {
				return totalUnread, err
			}

			underlying.SetMapIndex(reflect.ValueOf(header.Name), result.Elem())
		}

		return totalUnread, nil
	default:
		return 0, fmt.Errorf("%w a pointer to a struct or map")
	}

}

func createType(tagID TagID) interface{} {
	switch tagID {
	case TagByte:
		return byte(0)
	case TagShort:
		return int16(0)
	case TagInt:
		return int(0)
	case TagLong:
		return int64(0)
	case TagFloat:
		return float32(0)
	case TagDouble:
		return float64(0)
	case TagByteArray:
		return []byte{}
	case TagString:
		return ""
	case TagList:
		return []interface{}{}
	case TagCompound:
		return map[string]interface{}{}
	case TagIntArray:
		return []int{}
	case TagLongArray:
		return []int64{}
	default:
		panic("invalid tag ID")
	}
}

// ReadImmediate reads the next immediate value, assuming the header
// has already been read. Only use this if you know what you're doing!
func (r *Reader) ReadImmediate(tagID TagID, value interface{}) (int, error) {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return 0, fmt.Errorf("a non-nil pointer", ErrInvalidType)
	}

	// pointer in pointer, create a new value for it and redirect it
	if rv.Elem().Kind() == reflect.Ptr {
		newValue := reflect.New(rv.Elem().Elem().Type())
		rv.Elem().Set(newValue)
		rv = newValue.Addr()
	}

	if rv.Elem().Kind() == reflect.Interface && rv.Elem().NumMethod() == 0 {
		rv.Elem().Set(reflect.ValueOf(createType(tagID)))
		rv = rv.Elem().Elem().Addr()
		value = rv.Interface()
	}

	switch tagID {
	case TagEnd:
		return 0, ErrEndOfCompound
	case TagByte:
		v, ok := value.(*byte)
		if !ok {
			return 0, fmt.Errorf("%w byte", ErrInvalidType)
		}

		*v = r.data[r.cursor]
		r.cursor++
		return 1, nil
	case TagShort:
		v, ok := value.(*int16)
		if !ok {
			return 0, fmt.Errorf("%w int16", ErrInvalidType)
		}

		*v = int16(r.data[r.cursor])<<8 | int16(r.data[r.cursor+1])
		r.cursor += 2
		return 2, nil
	case TagInt:
		v, ok := value.(*int)
		if !ok {
			return 0, fmt.Errorf("%w int", ErrInvalidType)
		}

		*v = int(int32(r.readInt()))

		return 4, nil
	case TagLong:
		v, ok := value.(*int64)
		if !ok {
			return 0, fmt.Errorf("%w int64", ErrInvalidType)
		}

		*v = int64(r.readInt64())

		return 8, nil
	case TagFloat:
		v, ok := value.(*float32)
		if !ok {
			return 0, fmt.Errorf("%w float32", ErrInvalidType)
		}

		raw32 := r.readInt()
		*v = math.Float32frombits(raw32)

		return 4, nil
	case TagDouble:
		v, ok := value.(*float64)
		if !ok {
			return 0, fmt.Errorf("%w float64", ErrInvalidType)
		}

		raw64 := r.readInt64()
		*v = math.Float64frombits(raw64)

		return 8, nil
	case TagByteArray:
		v, ok := value.(*[]byte)
		if !ok {
			return 0, fmt.Errorf("%w []byte", ErrInvalidType)
		}

		length := int(r.readInt())

		*v = r.data[r.cursor : r.cursor+length]
		r.cursor += length

		return 4 + length, nil
	case TagString:
		v, ok := value.(*string)
		if !ok {
			return 0, fmt.Errorf("%w string", ErrInvalidType)
		}

		length := int(r.data[r.cursor])<<8 | int(r.data[r.cursor+1])

		r.cursor += 2
		*v = string(r.data[r.cursor : r.cursor+length])
		r.cursor += length

		return 2 + length, nil
	case TagList:
		tagID, length, unread := r.ReadListTagHeader()

		if rv.Elem().Kind() != reflect.Slice {
			return unread, fmt.Errorf("%w pointer to slice", ErrInvalidType)
		}

		result := reflect.MakeSlice(rv.Elem().Type(), length, length)

		for i := 0; i < length; i++ {
			elemUnread, err := r.ReadImmediate(tagID, result.Index(i).Addr().Interface())
			unread += elemUnread
			if err != nil {
				return unread, err
			}
		}

		rv.Elem().Set(result)

		return unread, nil
	case TagCompound:
		return r.readCompound(value)
	case TagIntArray:
		v, ok := value.(*[]int)
		if !ok {
			return 0, fmt.Errorf("%w []int", ErrInvalidType)
		}

		length := int(r.readInt())
		*v = make([]int, length)

		for i := 0; i < length; i++ {
			(*v)[i] = int(int32(r.readInt()))
		}

		return 4 + 4*length, nil
	case TagLongArray:
		v, ok := value.(*[]int64)
		if !ok {
			return 0, fmt.Errorf("%w []int64", ErrInvalidType)
		}

		length := int(r.readInt())
		*v = make([]int64, length)

		for i := 0; i < length; i++ {
			(*v)[i] = int64(r.readInt())
		}

		return 4 + 8*length, nil
	default:
		return 0, fmt.Errorf("%w [invalid tag ID]", ErrInvalidType)
	}
}

func (r *Reader) readInt() uint32 {
	num := (uint32(r.data[r.cursor])<<24 | uint32(r.data[r.cursor+1])<<16 |
		uint32(r.data[r.cursor+2])<<8 | uint32(r.data[r.cursor+3]))
	r.cursor += 4
	return num
}

func (r *Reader) readInt64() uint64 {
	num := uint64(r.data[r.cursor])<<56 | uint64(r.data[r.cursor+1])<<48 | uint64(r.data[r.cursor+2])<<40 |
		uint64(r.data[r.cursor+3])<<32 | uint64(r.data[r.cursor+4])<<24 | uint64(r.data[r.cursor+5])<<16 | uint64(r.data[r.cursor+6])<<8 |
		uint64(r.data[r.cursor+7])
	r.cursor += 8
	return num
}

func (r *Reader) SimpleMatch(pattern []byte, count int) []int {
	prevCursor := r.cursor
	defer func() {
		r.cursor = prevCursor
	}()

	r.cursor = 0
	total := 0

	var results []int

	for {
		nextPos := bytes.Index(r.data[r.cursor:], pattern)
		if nextPos < 0 {
			break
		}

		r.cursor += nextPos + 1
		total++

		results = append(results, r.cursor-1)

		if total > 0 && total >= count {
			break
		}
	}

	return results
}

// Only returns 1 result!!! if there are multiple possible results
// it only returns on possible candidate, it is not guaranteed to be correct!!
func (r *Reader) PossibleTagMatch(patterns [][][]byte) (bool, error) {
	maxLimit := len(r.data)

	for i := len(patterns) - 1; i >= 0; i-- {
		group := patterns[i]
		found := false

		for _, pat := range group {
			idx := bytes.LastIndex(r.data, pat)
			if idx < 0 {
				return false, nil
			}

			if idx < maxLimit {
				maxLimit = idx
				found = true
			}
		}

		if !found {
			return false, nil
		}
	}

	return true, nil
}

func (r *Reader) MatchTags(headerGroup [][]byte) ([]*IndexEntry, error) {
	if r.Index == nil {
		return nil, ErrNotIndexed
	}

	prevCursor := r.cursor
	defer func() {
		r.cursor = prevCursor
	}()

	r.cursor = 0
	var results []*IndexEntry

	for {
		nextPos := bytes.Index(r.data[r.cursor:], headerGroup[0])
		if nextPos < 0 {
			break
		}

		r.cursor += nextPos

		_, err := r.SkipTagHeader()
		if err != nil {
			fmt.Println("nbt: warning: malformed tag header?:", err)
			continue
		}

		meta, found := r.Index[r.cursor]
		if !found {
			log.Println("warning: matching tag not in index:", r.cursor)
			continue
		}

		if meta.Parent == nil {
			return nil, ErrIndexCorrupt
		}

		headerChecks := make([][]byte, len(headerGroup)-1)
		copy(headerChecks, headerGroup[1:])

		if len(headerChecks) > 0 {
			for _, child := range meta.Parent.Children {
				if child.Pos == r.cursor || child.ListIndex >= 0 {
					continue
				}

				childPos := child.Pos - child.Header.Length()

				for i, matchTo := range headerChecks {
					if len(r.data)-childPos < len(matchTo) {
						continue
					}

					if bytes.Equal(r.data[childPos:childPos+len(matchTo)], matchTo) {
						headerChecks[i] = headerChecks[len(headerChecks)-1]
						headerChecks = headerChecks[:len(headerChecks)-1]
						break
					}
				}
			}
		}

		if len(headerChecks) == 0 {
			results = append(results, r.Index[r.cursor])
		}
	}

	return results, nil
}

func (r *Reader) SimpleTagSize(tagID TagID) int {
	switch tagID {
	case TagEnd:
		return 0
	case TagByte:
		return 1
	case TagShort:
		return 2
	case TagInt, TagFloat:
		return 4
	case TagLong, TagDouble:
		return 8
	case TagByteArray:
		size := int(r.readInt())
		r.cursor -= 4
		return size + 4
	case TagString:
		return 2 + (int(r.data[r.cursor])<<8 | int(r.data[r.cursor+1]))
	case TagIntArray:
		size := int(r.readInt())
		r.cursor -= 4
		return size*4 + 4
	case TagLongArray:
		size := int(r.readInt())
		r.cursor -= 4
		return size*8 + 4
	default:
		panic(fmt.Sprintf("unsupported tag ID: %v", tagID))
	}
}

// SkipTag skips the given tag ID assuming the header has already been read.
// This is relatively safe for SkipTag(TagCompound), only use it for
// other tag IDs if you know what you're doing. This is an unsafe
// feature provided for high performance and fine control.
func (r *Reader) SkipTag(tagID TagID) {
	switch tagID {
	case TagEnd, TagByte, TagShort, TagInt, TagFloat, TagLong, TagDouble, TagByteArray,
		TagString, TagIntArray, TagLongArray:
		r.cursor += r.SimpleTagSize(tagID)
	case TagList:
		elemTag, length, _ := r.ReadListTagHeader()
		for i := 0; i < length; i++ {
			r.SkipTag(elemTag)
		}
	case TagCompound:
		// recursively tag skip
		for {
			tagHeader, _, _ := r.ReadTagHeader()
			if tagHeader.TagID == TagEnd {
				break
			}
			r.SkipTag(tagHeader.TagID)
		}
	default:
		panic(fmt.Sprintf("invalid tag ID: %v", tagID))
	}
}

func (r *Reader) ReadListTagHeader() (tagID TagID, length int, unreadLength int) {
	tagID = TagID(r.data[r.cursor])
	r.cursor++
	length = int(r.readInt())
	unreadLength = 5
	return
}

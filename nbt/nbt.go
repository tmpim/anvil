package nbt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
)

var (
	ErrEndOfCompound         = errors.New("nbt: end of compound")
	ErrInvalidType           = errors.New("nbt: invalid type, type must be:")
	ErrInvalidHeaderLocation = errors.New("nbt: invalid header location")
	ErrIndexCorrupt          = errors.New("nbt: index corrupt, report this to 1lann")
	ErrNotIndexed            = errors.New("nbt: indexing is required before calling this method")
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

type Reader struct {
	data   []byte
	cursor int
	Index  map[int]*IndexEntry
	// index []*IndexEntry
}

type IndexEntry struct {
	Parent    int
	Children  []int
	Header    TagHeader
	ListIndex int
}

type TagHeader struct {
	TagID TagID
	Name  []byte
}

func (t *TagHeader) Length() int {
	return 3 + len(t.Name)
}

type Breadcrumb struct {
	*IndexEntry
	Reader Reader
}

type Breadcrumbs []Breadcrumb

func (b Breadcrumbs) String() string {
	var result []byte

	for i := len(b) - 1; i >= 0; i-- {
		crumb := b[i]
		if crumb.ListIndex >= 0 {
			result = append(result, []byte("["+strconv.Itoa(crumb.ListIndex)+"]")...)
		} else {
			if i != len(b)-1 {
				result = append(result, '.')
			}
			result = append(result, crumb.Header.Name...)
		}
	}

	return string(result)
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

type SelectiveIndex []TagHeader

func (s SelectiveIndex) Matches(header TagHeader) bool {
	for _, selection := range s {
		if header.TagID == selection.TagID && bytes.Equal(header.Name, selection.Name) {
			return true
		}
	}

	return false
}

func (r *Reader) PrepareIndex(selectiveIndex SelectiveIndex) (err error) {
	if r.Index != nil {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("nbt: panic preparing index: %v", r)
		}
	}()

	r.Index = make(map[int]*IndexEntry)

	savedCursor := r.cursor

	r.Index[0] = &IndexEntry{
		Parent:    -1,
		ListIndex: -1,
		Header: TagHeader{
			TagID: TagEnd,
			Name:  []byte("root"),
		},
	}

	err = r.indexCompound(0, false, selectiveIndex)
	r.cursor = savedCursor
	if err != nil {
		return fmt.Errorf("nbt: error preparing index: %w", err)
	}
	return err
}

func (r *Reader) indexCompound(parent int, index bool, selectiveIndex SelectiveIndex) error {
	var par *IndexEntry
	if index {
		par = r.Index[parent]
	}

	for {
		header, _, err := r.ReadTagHeader()
		if err == io.EOF {
			return nil
		}

		if err != nil {
			return fmt.Errorf("error reading tag header at position %d: %w", r.cursor, err)
		}

		shouldIndex := index

		if !shouldIndex {
			shouldIndex = selectiveIndex.Matches(header)
		}

		if shouldIndex {
			if par != nil {
				r.Index[r.cursor] = &IndexEntry{
					ListIndex: -1,
					Parent:    parent,
					Header:    header,
				}
				par.Children = append(par.Children, r.cursor)
			} else {
				r.Index[r.cursor] = &IndexEntry{
					ListIndex: -1,
					Parent:    -1,
					Header:    header,
				}
			}
		}

		switch header.TagID {
		case TagEnd:
			return nil
		case TagCompound:
			if err := r.indexCompound(r.cursor, shouldIndex, selectiveIndex); err != nil {
				return err
			}
		case TagList:
			if err := r.indexList(r.cursor, shouldIndex, selectiveIndex); err != nil {
				return err
			}
		default:
			r.SkipTag(header.TagID)
		}
	}
}

func (r *Reader) indexList(parent int, index bool, selectiveIndex SelectiveIndex) error {
	tagID, length, unread := r.ReadListTagHeader()
	if tagID != TagCompound && tagID != TagList {
		r.Unread(unread)
		r.SkipTag(TagList)
		return nil
	}

	var par *IndexEntry
	if index {
		par = r.Index[parent]
	}

	if tagID == TagCompound {
		for i := 0; i < length; i++ {
			if index {
				r.Index[r.cursor] = &IndexEntry{
					ListIndex: i,
					Parent:    parent,
					Header: TagHeader{
						TagID: tagID,
						Name:  nil,
					},
				}
				par.Children = append(par.Children, r.cursor)
			}

			if err := r.indexCompound(r.cursor, index, selectiveIndex); err != nil {
				return err
			}
		}
	} else if tagID == TagList {
		for i := 0; i < length; i++ {
			if index {
				r.Index[r.cursor] = &IndexEntry{
					ListIndex: i,
					Parent:    parent,
					Header: TagHeader{
						TagID: tagID,
						Name:  nil,
					},
				}
				par.Children = append(par.Children, r.cursor)
			}

			if err := r.indexList(r.cursor, index, selectiveIndex); err != nil {
				return err
			}
		}
	}

	return nil
}

// Verifies and computes the breadcrumb to get to the current location.
func (r *Reader) Breadcrumbs() (Breadcrumbs, error) {
	if r.Index == nil {
		return nil, ErrNotIndexed
	}

	var results Breadcrumbs

	pos := r.cursor

	for {

		meta, found := r.Index[pos]
		if !found {
			if pos == r.cursor {
				return nil, ErrInvalidHeaderLocation
			}

			return nil, ErrIndexCorrupt
		}

		results = append(results, Breadcrumb{
			Reader:     r.Copy(pos),
			IndexEntry: meta,
		})

		pos = meta.Parent

		if pos < 0 {
			break
		}
	}

	return results, nil
}

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

		result := reflect.MakeSlice(rv.Elem().Type().Elem(), length, 0)

		for i := 0; i < length; i++ {
			elemUnread, err := r.ReadImmediate(tagID, result.Index(i).Addr().Interface())
			unread += elemUnread
			if err != nil {
				return unread, err
			}
		}

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

func (r *Reader) MatchTags(headerGroup [][]byte) ([]Breadcrumbs, error) {
	if r.Index == nil {
		return nil, ErrNotIndexed
	}

	prevCursor := r.cursor
	defer func() {
		r.cursor = prevCursor
	}()

	r.cursor = 0
	var results []Breadcrumbs

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
			continue
		}

		if meta.Parent < 0 {
			return nil, ErrIndexCorrupt
		}

		headerChecks := make([][]byte, len(headerGroup)-1)
		copy(headerChecks, headerGroup[1:])

		if len(headerChecks) > 0 {
			for _, childPos := range meta.Children {
				if childPos == r.cursor {
					continue
				}

				child, found := r.Index[childPos]
				if !found || child.ListIndex >= 0 {
					continue
				}

				childPos -= child.Header.Length()

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
			res := r.Copy(r.cursor)
			crumbs, err := res.Breadcrumbs()
			if err != nil {
				return nil, fmt.Errorf("nbt: error getting breadcrumbs?: %w", err)
			}
			results = append(results, crumbs)
		}
	}

	return results, nil
}

type BasicTag struct {
	Header TagHeader
	Value  []byte
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

// SkipTag skips the given tag ID assuming the header has already been read.
// This is relatively safe for SkipTag(TagCompound), only use it for
// other tag IDs if you know what you're doing. This is an unsafe
// feature provided for high performance and fine control.
func (r *Reader) SkipTag(tagID TagID) {
	switch tagID {
	case TagEnd:
	case TagByte:
		r.cursor++
	case TagShort:
		r.cursor += 2
	case TagInt, TagFloat:
		r.cursor += 4
	case TagLong, TagDouble:
		r.cursor += 8
	case TagByteArray:
		r.cursor += int(r.readInt())
	case TagString:
		r.cursor += 2 + (int(r.data[r.cursor])<<8 | int(r.data[r.cursor+1]))
	case TagIntArray:
		r.cursor += 4 * int(r.readInt())
	case TagLongArray:
		r.cursor += 8 * int(r.readInt())
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

func (r *Reader) ReadTagHeader() (tagHeader TagHeader, unreadLength int,
	err error) {
	// defer func() {
	// 	if r := recover(); r != nil {
	// 		log.Println("anvil/nbt: warning: panic:", r)
	// 		err = io.EOF
	// 	}
	// }()

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

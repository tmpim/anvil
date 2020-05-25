package nbt

//go:generate msgp

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/tinylib/msgp/msgp"
)

type FlatIndexEntry struct {
	P int
	A int
	C []int
	H *TagHeader
	I int
}

type IndexWrapper []FlatIndexEntry

type IndexEntry struct {
	Pos       int
	Parent    *IndexEntry
	Children  []*IndexEntry
	Header    TagHeader
	ListIndex int
}

type SelectiveIndex []TagHeader

func (s SelectiveIndex) Matches(header TagHeader) bool {
	for _, selection := range s {
		if header.TagID == selection.TagID && bytes.Equal(header.Name, selection.Name) {
			return true
		}
	}

	return false
}

func toPos(e *IndexEntry) int {
	if e == nil {
		return -1
	}
	return e.Pos
}

func toList(entries []*IndexEntry) []int {
	results := make([]int, len(entries))
	for i, v := range entries {
		results[i] = v.Pos
	}
	return results
}

// EncodeIndex encodes the index.
func (r *Reader) EncodeIndex() []byte {
	var flat IndexWrapper

	for _, v := range r.Index {
		flat = append(flat, FlatIndexEntry{
			P: v.Pos,
			A: toPos(v.Parent),
			C: toList(v.Children),
			H: &v.Header,
			I: v.ListIndex,
		})
	}

	buf := new(bytes.Buffer)
	err := msgp.Encode(buf, flat)
	if err != nil {
		panic(err)
	}

	return buf.Bytes()
}

func (r *Reader) FastPrepareIndex() (err error) {
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

	header, _, err := r.ReadTagHeader()
	if err != nil {
		return err
	}

	root := &IndexEntry{
		Pos:       r.cursor,
		Parent:    nil,
		ListIndex: -1,
		Header:    header,
	}
	r.Index[r.cursor] = root

	switch header.TagID {
	case TagCompound:
		if err = r.indexCompound(root, true, nil); err != nil {
			return err
		}
	case TagList:
		if err = r.indexList(root, true, nil); err != nil {
			return err
		}
	default:
		err = errors.New("nbt: invalid tag ID for fast prepare index, must be compound or list")
	}

	r.cursor = savedCursor
	if err != nil {
		return fmt.Errorf("nbt: error preparing index: %w", err)
	}
	return err
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

	root := &IndexEntry{
		Pos:       0,
		Parent:    nil,
		ListIndex: -1,
		Header: TagHeader{
			TagID: TagCompound,
			Name:  []byte("root"),
		},
	}
	r.Index[0] = root

	index := false
	if selectiveIndex == nil {
		index = true
	}

	err = r.indexCompound(root, index, selectiveIndex)
	r.cursor = savedCursor
	if err != nil {
		return fmt.Errorf("nbt: error preparing index: %w", err)
	}
	return err
}

func (r *Reader) indexCompound(parent *IndexEntry, index bool, selectiveIndex SelectiveIndex) error {
	for {
		header, _, err := r.ReadTagHeader()
		if err == io.EOF {
			return nil
		}

		if err != nil {
			return fmt.Errorf("error reading tag header at position %d: %w", r.cursor, err)
		}

		if header.TagID == TagEnd {
			return nil
		}

		shouldIndex := index

		if !shouldIndex {
			shouldIndex = selectiveIndex.Matches(header)
		}

		ent := &IndexEntry{
			Pos:       r.cursor,
			ListIndex: -1,
			Parent:    parent,
			Header:    header,
		}

		if shouldIndex {
			r.Index[r.cursor] = ent
			if parent != nil {
				parent.Children = append(parent.Children, ent)
			}
		}

		switch header.TagID {
		case TagCompound:
			if err := r.indexCompound(ent, shouldIndex, selectiveIndex); err != nil {
				return err
			}
		case TagList:
			if err := r.indexList(ent, shouldIndex, selectiveIndex); err != nil {
				return err
			}
		default:
			r.SkipTag(header.TagID)
		}
	}
}

func (r *Reader) indexList(parent *IndexEntry, index bool, selectiveIndex SelectiveIndex) error {
	tagID, length, unread := r.ReadListTagHeader()
	if tagID != TagCompound && tagID != TagList {
		r.Unread(unread)
		r.SkipTag(TagList)
		return nil
	}

	if tagID == TagCompound {
		for i := 0; i < length; i++ {
			ent := &IndexEntry{
				Pos:       r.cursor,
				ListIndex: i,
				Parent:    parent,
				Header: TagHeader{
					TagID: tagID,
					Name:  nil,
				},
			}

			if index {
				r.Index[r.cursor] = ent
				if parent != nil {
					parent.Children = append(parent.Children, ent)
				}
			}

			if err := r.indexCompound(ent, index, selectiveIndex); err != nil {
				return err
			}
		}
	} else if tagID == TagList {
		for i := 0; i < length; i++ {
			ent := &IndexEntry{
				Pos:       r.cursor,
				ListIndex: i,
				Parent:    parent,
				Header: TagHeader{
					TagID: tagID,
					Name:  nil,
				},
			}

			if index {
				r.Index[r.cursor] = ent
				if parent != nil {
					parent.Children = append(parent.Children, ent)
				}
			}

			if err := r.indexList(ent, index, selectiveIndex); err != nil {
				return err
			}
		}
	}

	return nil
}

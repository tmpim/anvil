package nbt

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"io"

	"github.com/ppacher/nbt"
	"github.com/tmpim/anvil"
)

func (r *Reader) StructureToJSON(entry *IndexEntry) []byte {
	results := map[string]interface{}{
		string(entry.Header.Name): r.recursiveIface(entry),
	}

	data, err := json.Marshal(results)
	if err != nil {
		panic(err)
	}

	return data
}

func (r *Reader) recursiveIface(entry *IndexEntry) interface{} {
	switch entry.Header.TagID {
	case TagList:
		results := make([]interface{}, len(entry.Children))
		for i, child := range entry.Children {
			results[i] = r.recursiveIface(child)
		}
		return results
	case TagCompound:
		results := make(map[string]interface{})
		for _, child := range entry.Children {
			results[string(child.Header.Name)] = r.recursiveIface(child)
		}
		return results
	default:
		return entry.Header.TagID
	}
}

type TileEntityDetails struct {
	Location  anvil.Coord
	Container bool
	Count     int
}

func (r *Reader) GetTileEntityDetails(ent *IndexEntry) *TileEntityDetails {
	xStr := []byte("x")
	yStr := []byte("y")
	zStr := []byte("z")
	countStr := []byte("Count")

	var foundCoord anvil.Coord

	var count byte
	foundLocation := false
	cur := ent.Parent

	for cur != nil {
		var x, y, z int
		found := 0

		for _, child := range cur.Children {
			name := child.Header.Name
			if bytes.Equal(name, xStr) && !foundLocation {
				r.SeekTo(child.Pos)
				_, err := r.ReadImmediate(nbt.TagInt, &x)
				if err != nil {
					panic(err)
				}
				found++
			} else if bytes.Equal(name, yStr) && !foundLocation {
				r.SeekTo(child.Pos)
				_, err := r.ReadImmediate(nbt.TagInt, &y)
				if err != nil {
					panic(err)
				}
				found++
			} else if bytes.Equal(name, zStr) && !foundLocation {
				r.SeekTo(child.Pos)
				_, err := r.ReadImmediate(nbt.TagInt, &z)
				if err != nil {
					panic(err)
				}
				found++
			} else if bytes.Equal(name, countStr) && count == 0 {
				r.SeekTo(child.Pos)
				_, err := r.ReadImmediate(nbt.TagByte, &count)
				if err != nil {
					panic(err)
				}
			}
		}

		if found == 3 {
			foundLocation = true
			foundCoord = anvil.Coord{x, y, z}

			if cur == ent.Parent {
				return &TileEntityDetails{
					Location:  foundCoord,
					Container: false,
					Count:     1,
				}
			}
		}

		cur = cur.Parent
	}

	if foundLocation && count == 0 {
		return &TileEntityDetails{
			Location:  foundCoord,
			Container: true,
			Count:     1,
		}
	} else if foundLocation {
		return &TileEntityDetails{
			Location:  foundCoord,
			Container: true,
			Count:     int(count),
		}
	}

	return nil
}

func NewTileEntitiesReader(data *anvil.ChunkData) (Reader, error) {
	rd, err := zlib.NewReader(bytes.NewReader(data.Data))
	if err != nil {
		return Reader{}, err
	}
	defer rd.Close()

	target := (&TagHeader{
		TagID: nbt.TagList,
		Name:  []byte("TileEntities"),
	}).Bytes()

	buf := make([]byte, 16384)
	readBuf := buf[len(target):]
	for {
		n, err := io.ReadAtLeast(rd, readBuf, len(target))
		if err == io.EOF {
			return Reader{}, io.ErrUnexpectedEOF
		} else if err != nil {
			return Reader{}, err
		}

		res := bytes.Index(buf, target)
		if res < 0 {
			copy(buf[:len(target)], readBuf[n-len(target):n])
			continue
		}

		buf = buf[res : len(target)+n]
		break
	}

	bbuf := bytes.NewBuffer(buf)

	endTarget := (&TagHeader{
		TagID: nbt.TagList,
		Name:  []byte("Entities"),
	}).Bytes()

	endRd := io.TeeReader(rd, bbuf)

	buf = make([]byte, 4096)
	readBuf = buf[len(endTarget):]
	for {
		n, err := io.ReadAtLeast(endRd, readBuf, len(endTarget))
		if err != nil {
			endRd.Read(buf)
			return NewReader(bbuf.Bytes()), nil
		}

		res := bytes.Index(buf, endTarget)
		if res < 0 {
			copy(buf[:len(endTarget)], readBuf[n-len(endTarget):n])
			continue
		}

		return NewReader(bbuf.Bytes()), nil
	}
}

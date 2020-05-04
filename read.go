package anvil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zlib"
	"github.com/minio/highwayhash"
	"github.com/tmpim/anvil/nbt"
)

const (
	sectorShift = 12
)

var hashKey = []byte("\x8f\x7f\x9e\x63\x9f\x74\x8a\xc3\xe4\x21\xe8\xda\x7a\x7e\xbc\x12\x3a\xec\x2e\x15\xc4\xf4\x7d\x18\x8c\x7e\x2d\xf0\x86\x01\x26\xd9")

type ChunkData struct {
	Chunk Chunk
	Data  []byte
}

type RegionReader struct {
	Region Region
	header []byte
	file   *os.File
}

func (c *ChunkData) Hash() [highwayhash.Size128]byte {
	return highwayhash.Sum128(c.Data, hashKey)
}

func (c *ChunkData) Decompress() ([]byte, error) {
	rd, err := zlib.NewReader(bytes.NewReader(c.Data))
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	return ioutil.ReadAll(rd)
}

func (c *ChunkData) NBTReader() (nbt.Reader, error) {
	data, err := c.Decompress()
	if err != nil {
		return nbt.Reader{}, err
	}

	return nbt.NewReader(data), nil
}

func OpenRegionFile(filename string) (*RegionReader, error) {
	region, err := validateFilename(filename)
	if err != nil {
		return nil, fmt.Errorf("anvil: not a valid region filename: %w", err)
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	header := make([]byte, 4096)
	_, err = io.ReadFull(f, header)
	if err != nil {
		return nil, err
	}

	return &RegionReader{
		Region: region,
		header: header,
		file:   f,
	}, nil
}

func (r *RegionReader) Close() error {
	return r.file.Close()
}

func (r *RegionReader) ReadChunk(chunk Chunk) (ChunkData, error) {
	offset := chunk.RegionChunkOffset()
	data, err := r.readRawChunk(offset)
	if err != nil {
		return ChunkData{}, err
	}

	return ChunkData{
		Chunk: r.Region.OffsetToChunk(offset),
		Data:  data,
	}, nil
}

func (r *RegionReader) readRawChunk(offset int) ([]byte, error) {
	pos := (int(r.header[offset])<<16 | int(r.header[offset+1])<<8 |
		int(r.header[offset+2])) << sectorShift

	var chunkHeader [5]byte // force a stack allocation

	if _, err := r.file.Seek(int64(pos), 0); err != nil {
		return nil, err
	}

	_, err := io.ReadFull(r.file, chunkHeader[:])
	if err != nil {
		return nil, err
	}

	length := (int(chunkHeader[0])<<24 | int(chunkHeader[1])<<16 |
		int(chunkHeader[2])<<8 | int(chunkHeader[3]))

	data := make([]byte, length)

	_, err = io.ReadFull(r.file, data)
	if err != nil {
		return nil, err
	}

	// fmt.Printf("zlib header: %02x %02x\n", data[0], data[1])

	return data, nil
}

// caller responsibility to close(results)
func (r *RegionReader) ReadAllChunks(results chan<- ChunkData) error {
	region := r.Region

	for i := 0; i < 4096; i += 4 {
		data, err := r.readRawChunk(i)
		if err != nil {
			return err
		}

		c := ChunkData{
			Chunk: region.OffsetToChunk(i),
			Data:  data,
		}

		results <- c
	}

	return nil
}

func validateFilename(filename string) (region Region, err error) {
	parts := strings.Split(filepath.Base(filename), ".")
	if len(parts) != 4 {
		err = errors.New("must have 4 dot seperated components")
		return
	}

	if parts[0] != "r" {
		err = errors.New("first component must be \"r\"")
		return
	}

	if parts[3] != "mca" {
		err = errors.New("extension must be \"mca\"")
		return
	}

	region.X, err = strconv.Atoi(parts[1])
	if err != nil {
		return
	}

	region.Z, err = strconv.Atoi(parts[2])

	return
}

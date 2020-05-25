package anvil

import "math"

type Coord struct {
	X int
	Y int
	Z int
}

type Chunk struct {
	X int
	Y int
	Z int
}

type Region struct {
	X int
	Z int
}

func (c *Chunk) RegionChunkOffset() int {
	return ((c.X & 0b11111) | (c.Z&0b11111)<<5) << 2
}

func (c *Coord) Chunk() Chunk {
	return Chunk{
		X: c.X >> 4,
		Z: c.Z >> 4,
	}
}

func (c *Coord) Region() Region {
	return Region{
		X: c.X >> 9,
		Z: c.Z >> 9,
	}
}

func (c *Chunk) Region() Region {
	return Region{
		X: c.X >> 5,
		Z: c.Z >> 5,
	}
}

func (c *Chunk) CornerCoord() Coord {
	return Coord{
		X: c.X << 4,
		Y: c.Y << 4,
		Z: c.Z << 4,
	}
}

func (r *Region) CornerChunk() Chunk {
	return Chunk{
		X: r.X << 5,
		Z: r.Z << 5,
	}
}

func (c *Coord) Dist(other *Coord) float64 {
	return math.Sqrt(float64((c.X-other.X)*(c.X-other.X) +
		(c.Y-other.Y)*(c.Y-other.Y) + (c.Z-other.Z)*(c.Z-other.Z)))
}

// offset is from [0, 4092]
func (r *Region) OffsetToChunk(offset int) Chunk {
	chunkX := (offset >> 2) & 0b11111
	chunkZ := (offset >> 7) & 0b11111

	return Chunk{
		X: r.X<<5 | chunkX,
		Y: 0,
		Z: r.Z<<5 | chunkZ,
	}
}

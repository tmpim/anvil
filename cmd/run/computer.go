package main

import (
	"bytes"

	"github.com/tmpim/anvil"
	"github.com/tmpim/anvil/nbt"
)

type FoundComputer struct {
	ID          int
	Coord       anvil.Coord
	Container   bool
	Approximate bool
}

func resolveComputer(nrd *nbt.Reader, ent *nbt.IndexEntry) []FoundComputer {
	if !bytes.Equal(ent.Header.Name, []byte("computerID")) {
		panic("breadcrumb must be to a computerID")
	}

	var computerID int

	nrd.SeekTo(ent.Pos)
	_, err := nrd.ReadImmediate(nbt.TagInt, &computerID)
	if err != nil {
		panic(err)
	}

	details := nrd.GetTileEntityDetails(ent)
	if details == nil {
		return nil
	}

	results := make([]FoundComputer, details.Count)
	for i := range results {
		results[i] = FoundComputer{
			ID:          computerID,
			Coord:       details.Location,
			Container:   details.Container,
			Approximate: false,
		}
	}

	return results
}

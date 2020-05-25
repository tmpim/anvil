package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tmpim/anvil/nbt"
)

type PlayerFile struct {
	Player string
	Data   []byte
}

type PlayerComputer struct {
	ComputerID int
	Player     string
}

// minX, maxX, minZ, maxZ: [-7268, 7732, -7496, 7504]

func main() {
	if len(os.Args) < 2 {
		log.Println("specify the player data folder pls")
		return
	}

	start := time.Now()

	files, err := ioutil.ReadDir(os.Args[1])
	if err != nil {
		panic(err)
	}

	var playerFiles []string

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".dat" {
			playerFiles = append(playerFiles, filepath.Join(os.Args[1], file.Name()))
		}
	}

	if len(playerFiles) == 0 {
		fmt.Println("no players found??? did you specify the right dir?")
		os.Exit(1)
	}

	out := make(chan PlayerFile, 10)

	go func() {
		defer close(out)

		for _, file := range playerFiles {
			data, err := ioutil.ReadFile(file)
			if err != nil {
				log.Printf("failed to read player file %q: %v", file, err)
				continue
			}

			uuid := strings.Split(filepath.Base(file), ".")[0]

			out <- PlayerFile{
				Player: uuid,
				Data:   data,
			}
		}
	}()

	wg := new(sync.WaitGroup)
	statsMutex := new(sync.Mutex)
	stats := make(map[string]int)

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for playerfile := range out {
				nrd, err := nbt.NewGzipReader(bytes.NewReader(playerfile.Data))
				if err != nil {
					log.Printf("failed to ungzip player file %q: %v\n", playerfile.Player, err)
					continue
				}

				results := nrd.SimpleMatch([]byte("The Transreich Trade Agreement"), -1)
				if len(results) == 0 {
					continue
				}

				err = nrd.PrepareIndex(nil)
				if err != nil {
					panic(err)
				}

				for _, res := range results {
					nrd.SeekTo(res)
					idx := nrd.AlignToIndex()
					nrd.SeekTo(idx.Pos)

					if idx.Header.TagID == nbt.TagString {
						var result string
						nrd.ReadImmediate(nbt.TagString, &result)
						fmt.Println(result)

						statsMutex.Lock()
						stats[result]++
						statsMutex.Unlock()
					}

					// if idx.Header.TagID == nbt.TagString {
					// 	var title string
					// 	nrd.ReadImmediate(nbt.TagString, &title)
					// 	fmt.Println(title, ":", playerfile.Player)

					// }
				}

			}
		}()
	}

	wg.Wait()

	// var stats runtime.MemStats
	// runtime.ReadMemStats(&stats)
	// fmt.Printf("%+v\n", stats)

	log.Println("took:", time.Since(start))

	for k, v := range stats {
		fmt.Println(k, ":", v)
	}
}

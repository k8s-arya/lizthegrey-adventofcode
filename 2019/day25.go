package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/lizthegrey/adventofcode/2019/intcode"
	"os"
	"sort"
	"strings"
	"sync"
)

var inputFile = flag.String("inputFile", "inputs/day25.input", "Relative file path to use as input.")
var debug = flag.Bool("debug", true, "Whether to print debug output.")

var disallow = []string{
	"giant electromagnet",
	"escape pod",
	"photons",
	"molten lava",
	"infinite loop",
}

type Loc string
type Maze map[Loc]map[Direction]Exit
type Exit struct {
	src      Loc
	dst      Loc
	resolved bool
	dir      Direction
}
type Direction string

func (ex Exit) Invert(arrivedAt Loc) Exit {
	var newDir Direction
	switch ex.dir {
	case "north":
		newDir = "south"
	case "east":
		newDir = "west"
	case "south":
		newDir = "north"
	case "west":
		newDir = "east"
	}
	return Exit{arrivedAt, ex.src, true, newDir}
}

func (m Maze) Move(l Loc, dir Direction) (Loc, bool) {
	if !m[l][dir].resolved {
		return Loc(""), false
	}
	return m[l][dir].dst, true
}

func main() {
	flag.Parse()
	tape := intcode.ReadInput(*inputFile)
	if tape == nil {
		fmt.Println("Failed to parse input.")
		return
	}

	inputs := 0

	input := make(chan int, 1)
	output, done := tape.Process(input)
	lines := make(chan string, 50)
	driver := make(chan string)
	reallyFinished := make(chan bool)

	go func() {
		line := make([]byte, 0)
		for c := range output {
			if *debug {
				fmt.Printf("%c", c)
			}

			if c == '\n' {
				if len(line) > 0 && line[0] == '"' {
					fmt.Println(string(line))
					fmt.Printf("Completed with %d inputs.\n", inputs)
					break
				}
				lines <- string(line)
				line = make([]byte, 0)
			} else {
				line = append(line, byte(c))
			}
		}
		if *debug {
			fmt.Println()
		}
		<-done
		reallyFinished <- true
	}()

	if *debug {
		go func() {
			reader := bufio.NewReader(os.Stdin)
			for {
				line, err := reader.ReadString('\n')
				if line == "\n" || err != nil {
					return
				}
				for _, r := range line {
					input <- int(r)
				}
			}
		}()
	}

	go func() {
		for l := range driver {
			inputs++
			if *debug {
				fmt.Printf("[Automated]: %s\n", l)
			}
			for _, r := range l {
				input <- int(r)
			}
			input <- int('\n')
		}
	}()

	// Always pick up items unless on disallow
	// Stop and wait for human if we encounter unexpected problems.
	go func() {
		var loc Loc
		rooms := make(Maze)
		seenItems := make(map[string]bool)
		var allItems, items []string
		var outbound []Direction
		var q Queue
		var arrived *Exit
		checkpointTry := 0

		parseMode := 0
	parse:
		for l := range lines {
			if l == "" {
				parseMode = 0
				continue
			}
			if l == "You can't go that way." {
				fmt.Println("Disabling automatic system.")
				return
			}
			if l[0] == '=' {
				loc = Loc(l[3 : len(l)-3])
				if rooms[loc] == nil {
					rooms[loc] = make(map[Direction]Exit)
				}
				items = nil
				outbound = nil
				if arrived != nil {
					rooms[arrived.src][arrived.dir] = Exit{arrived.src, loc, true, arrived.dir}
					backTrack := arrived.Invert(loc)
					rooms[loc][backTrack.dir] = backTrack
					arrived = nil
				}
				parseMode = 0
			} else if l[0] == '-' {
				if parseMode == 1 {
					outbound = append(outbound, Direction(l[2:]))
				} else if parseMode == 2 {
					items = append(items, l[2:])
				} else if parseMode == 3 {
					// Do nothing.
				} else {
					fmt.Println("Didn't know what to do with a list outside parse mode.")
				}
			} else if l[len(l)-1] == ':' {
				// This is a menu ("Doors here lead:" or "Items here:").
				keywords := strings.Split(l[:len(l)-1], " ")
				keyword := keywords[0]
				switch keyword {
				case "Doors":
					parseMode = 1
				case "Items":
					if keywords[1] == "here" {
						parseMode = 2
					} else {
						parseMode = 3
					}
				default:
					fmt.Println("Unknown menu type.")
				}
			} else if l[len(l)-1] == '?' {
				// Program finally is waiting on our input. Decide what to do next.
				for len(items) > 0 {
					item := items[0]
					skip := false
					for _, v := range disallow {
						if v == item {
							skip = true
							break
						}
					}
					if len(items) > 1 {
						items = items[1:]
					} else {
						items = nil
					}
					if !skip {
						driver <- fmt.Sprintf("take %s", item)
						if !seenItems[item] {
							allItems = append(allItems, item)
							seenItems[item] = true
						}
						continue parse
					}
				}
				if len(outbound) > 0 {
					for _, dir := range outbound {
						if _, ok := rooms[loc][dir]; !ok {
							// Throw it onto our exploration queue.
							exit := Exit{loc, "", false, dir}
							notifyRoom(&q, exit, rooms)
						}
					}
					// Pick a direction to move.
					next := nextDirection(&q, loc, rooms)
					if q.target != nil && next == q.target.dir && loc == q.target.src {
						arrived = q.target
						q.target = nil
					}
					if next == "dance" {
						// Now we can move onto phase 2 and access the security checkpoint.
						next = "west"
						sort.Strings(allItems)
						for _, v := range itemsToDrop(allItems, checkpointTry) {
							driver <- fmt.Sprintf("drop %s", v)
							<-lines
							<-lines
							<-lines
							<-lines
						}
						checkpointTry++
					}
					driver <- string(next)
				} else {
					if *debug {
						fmt.Println("Got into somewhere we can't leave")
					}
				}
			} else {
				// This is presumed to be flavor text or a response to command.
				// Just let the program print.
			}
		}
	}()
	<-reallyFinished
}

type Queue struct {
	mtx    sync.Mutex
	list   []Exit
	target *Exit
	sensor *Exit
}

func notifyRoom(q *Queue, ex Exit, rooms Maze) {
	q.mtx.Lock()
	defer q.mtx.Unlock()
	q.list = append(q.list, ex)
	if ex.src == "Security Checkpoint" {
		q.sensor = &ex
		return
	}
	rooms[ex.src][ex.dir] = ex
}

func nextDirection(q *Queue, loc Loc, rooms Maze) Direction {
	// Keep track of what's unexplored, and backtrack to try to find unexplored nodes.
	// Use a DFS in order to avoid repeated backtracking.
	q.mtx.Lock()
	defer q.mtx.Unlock()

	if q.target != nil {
		// We are in the middle of pathing somewhere...
		// TODO: read from the path cache.
		return bfs(loc, *q.target, rooms)[0]
	}
	// We've reached our destination and need a new destination.
	if len(q.list) == 0 {
		return "dance"
	}

	var toExplore Exit
	for {
		toExplore = q.list[0]
		if _, ok := rooms.Move(toExplore.src, toExplore.dir); ok {
			// Short circuit if we've already walked through that door.
			q.list = q.list[1:]
			continue
		}
		break
	}
	if len(q.list) > 1 {
		q.list = q.list[1:]
	} else {
		q.list = nil
	}
	q.target = &toExplore
	// TODO: write to the path cache.
	return bfs(loc, *q.target, rooms)[0]
}

func bfs(src Loc, dst Exit, rooms Maze) []Direction {
	// Perform a breadth-first search.
	shortest := map[Loc][]Direction{
		src: []Direction{},
	}
	worklist := []Loc{src}
	for {
		w := worklist[0]
		if w == dst.src {
			directions := make([]Direction, len(shortest[w])+1)
			copy(directions, shortest[w])
			directions[len(shortest[w])] = dst.dir
			return directions
		}
		for _, exit := range rooms[w] {
			moved, resolved := rooms.Move(w, exit.dir)
			if !resolved {
				continue
			}
			if _, ok := shortest[moved]; ok {
				continue
			}
			directions := make([]Direction, len(shortest[w])+1)
			copy(directions, shortest[w])
			directions[len(shortest[w])] = exit.dir
			shortest[moved] = directions
			worklist = append(worklist, moved)
		}
		worklist = worklist[1:]
	}
}

func itemsToDrop(all []string, try int) []string {
	var ret []string
	for k := range all {
		if try%2 == 1 {
			ret = append(ret, all[k])
		}
		try >>= 1
	}
	return ret
}

// Package ahocorasick implements the Aho-Corasick string matching algorithm for
// efficiently finding all instances of multiple patterns in a text.
package ahocorasick

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"sync"
)

const (
	// leaf represents a leaf on the trie
	// This must be <255 since the offsets used are in [0,255]
	// This should only appear in the Base array since the Check array uses
	// negative values to represent free states.
	leaf = -1867
)

type SWord struct {
	Len uint64
	Key uint64
}

// Matcher is the pattern matching state machine.
type Matcher struct {
	base   []int     // base array in the double array trie
	check  []int     // check array in the double array trie
	fail   []int     // fail function
	output [][]SWord // output function: originally [state][wordlen], replaced to tuple of {wordlen,workey}
}

// added function for byte serialization of compiled matcher
func (m *Matcher) Serialize() []byte {
	var lenBase, lenCheck, lenFail, lenOutput uint64

	lenBase = uint64(len(m.base))
	lenCheck = uint64(len(m.check))
	lenFail = uint64(len(m.fail))
	lenOutput = uint64(len(m.output))

	lenOutputEach := make([]uint64, lenOutput)

	for i, v := range m.output {
		lenOutputEach[i] = uint64(len(v))
	}

	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.LittleEndian, lenBase)
	if err != nil {
		fmt.Println("binary.Write failed for lenBase:", err)
	}
	err = binary.Write(buf, binary.LittleEndian, lenCheck)
	if err != nil {
		fmt.Println("binary.Write failed for lenCheck:", err)
	}
	err = binary.Write(buf, binary.LittleEndian, lenFail)
	if err != nil {
		fmt.Println("binary.Write failed for lenFail:", err)
	}
	err = binary.Write(buf, binary.LittleEndian, lenOutput) //2d array
	if err != nil {
		fmt.Println("binary.Write failed lenOutput:", err)
	}

	for i, v := range lenOutputEach {
		err = binary.Write(buf, binary.LittleEndian, uint64(v))
		if err != nil {
			fmt.Printf("binary.Write failed for lenOutputEach: %s at position %d", err, i)
		}
	}

	for i, v := range m.base {
		err = binary.Write(buf, binary.LittleEndian, uint64(v))
		if err != nil {
			fmt.Printf("binary.Write failed: %s at base, position %d", err, i)
		}
	}
	for i, v := range m.check {
		err = binary.Write(buf, binary.LittleEndian, uint64(v))
		if err != nil {
			fmt.Printf("binary.Write failed: %s at check, position %d", err, i)
		}
	}
	for i, v := range m.fail {
		err = binary.Write(buf, binary.LittleEndian, uint64(v))
		if err != nil {
			fmt.Printf("binary.Write failed: %s at fail, position %d", err, i)
		}
	}
	for i, v := range m.output {
		for j, u := range v {
			err = binary.Write(buf, binary.LittleEndian, u)
			if err != nil {
				fmt.Printf("binary.Write failed: %s at output, position %d, %d", err, i, j)
			}
		}
	}
	return (buf.Bytes())
}

type DeserializeError struct{}

func (m *DeserializeError) Error() string {
	return "Finite state machine is corrupted"
}

func Deserialize(data []byte) (m *Matcher, err error) {
	m = new(Matcher)

	totalLength := len(data)

	if totalLength < 32 || totalLength%8 != 0 {
		err = &DeserializeError{}
		return
	}
	//reader := bytes.NewReader(data)
	reader := bytes.NewReader(data)

	var lenBase, lenCheck, lenFail, lenOutput uint64

	err = binary.Read(reader, binary.LittleEndian, &lenBase)
	if err != nil {
		return
	}
	err = binary.Read(reader, binary.LittleEndian, &lenCheck)
	if err != nil {
		return
	}
	err = binary.Read(reader, binary.LittleEndian, &lenFail)
	if err != nil {
		return
	}
	err = binary.Read(reader, binary.LittleEndian, &lenOutput)
	if err != nil {
		return
	}

	lenOutputEach := make([]uint64, lenOutput)

	if totalLength < 8*(4+int(lenOutput)) {
		err = &DeserializeError{}
		return
	}

	calculatedLength := 8 * (4 + int(lenOutput) + int(lenBase) + int(lenCheck) + int(lenFail))

	for i := 0; i < int(lenOutput); i++ {
		err = binary.Read(reader, binary.LittleEndian, &(lenOutputEach[i]))
		if err != nil {
			return
		}
		calculatedLength += 16 * int(lenOutputEach[i])
	}

	if calculatedLength != totalLength {
		err = &DeserializeError{}
		return
	}

	err = readToSlice(reader, lenBase, &m.base)
	if err != nil {
		return
	}
	err = readToSlice(reader, lenCheck, &m.check)
	if err != nil {
		return
	}
	err = readToSlice(reader, lenFail, &m.fail)
	if err != nil {
		return
	}
	m.output = make([][]SWord, lenOutput)
	for i, v := range lenOutputEach {
		err = readToSliceSWord(reader, v, &m.output[i])
		if err != nil {
			return
		}
	}

	return
}

func readToSlice(reader *bytes.Reader, len uint64, array *[]int) error {
	*array = make([]int, len)
	var item uint64
	for i := 0; i < int(len); i++ {
		err := binary.Read(reader, binary.LittleEndian, &item)
		if err != nil {
			return err
		}
		(*array)[i] = int(item)
	}
	return nil
}

func readToSliceSWord(reader *bytes.Reader, len uint64, array *[]SWord) error {
	*array = make([]SWord, len)
	var item uint64
	var err error
	for i := 0; i < int(len); i++ {
		err = binary.Read(reader, binary.LittleEndian, &item)
		if err != nil {
			return err
		}
		(*array)[i].Len = item
		err = binary.Read(reader, binary.LittleEndian, &item)
		if err != nil {
			return err
		}
		(*array)[i].Key = item
	}
	return nil
}

func (m *Matcher) String() string {
	return fmt.Sprintf(`
Base:   %v
Check:  %v
Fail:   %v
Output: %v
`, m.base, m.check, m.fail, m.output)
}

type byteSliceSlice [][]byte

func (bss byteSliceSlice) Len() int           { return len(bss) }
func (bss byteSliceSlice) Less(i, j int) bool { return bytes.Compare(bss[i], bss[j]) < 1 }
func (bss byteSliceSlice) Swap(i, j int)      { bss[i], bss[j] = bss[j], bss[i] }

func compile(words [][]byte) *Matcher {
	m := new(Matcher)
	m.base = make([]int, 2048)[:1]
	m.check = make([]int, 2048)[:1]
	m.fail = make([]int, 2048)[:1]
	m.output = make([][]SWord, 2048)[:1]

	sort.Sort(byteSliceSlice(words))

	// Represents a node in the implicit trie of words
	type trienode struct {
		state int
		depth int
		start int
		end   int
	}
	queue := make([]trienode, 2048)[:1]
	queue[0] = trienode{0, 0, 0, len(words)}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		if node.end <= node.start {
			m.base[node.state] = leaf
			continue
		}

		var edges []byte
		for i := node.start; i < node.end; i++ {
			if len(edges) == 0 || edges[len(edges)-1] != words[i][node.depth] {
				edges = append(edges, words[i][node.depth])
			}
		}

		// Calculate a suitable Base value where each edge will fit into the
		// double array trie
		base := m.findBase(edges)
		m.base[node.state] = base

		i := node.start
		for _, edge := range edges {
			offset := int(edge)
			newState := base + offset

			m.occupyState(newState, node.state)

			// level 0 and level 1 should fail to state 0
			if node.depth > 0 {
				m.setFailState(newState, node.state, offset)
			}
			m.unionFailOutput(newState, m.fail[newState])

			// Add the child nodes to the queue to continue down the BFS
			newnode := trienode{newState, node.depth + 1, i, i}
			for {
				if newnode.depth >= len(words[i]) {
					m.output[newState] = append(m.output[newState], SWord{uint64(len(words[i])), uint64(i)})
					newnode.start++
				}
				newnode.end++

				i++
				if i >= node.end || words[i][node.depth] != edge {
					break
				}
			}
			queue = append(queue, newnode)
		}
	}

	return m
}

// CompileByteSlices compiles a Matcher from a slice of byte slices. This Matcher can be
// used to find occurrences of each pattern in a text.
func CompileByteSlices(words [][]byte) *Matcher {
	return compile(words)
}

// CompileStrings compiles a Matcher from a slice of strings. This Matcher can
// be used to find occurrences of each pattern in a text.
func CompileStrings(words []string) *Matcher {
	var wordByteSlices [][]byte
	for _, word := range words {
		wordByteSlices = append(wordByteSlices, []byte(word))
	}
	return compile(wordByteSlices)
}

// occupyState will correctly occupy state so it maintains the
// index=check[base[index]+offset] identity. It will also update the
// bidirectional link of free states correctly.
// Note: This MUST be used instead of simply modifying the check array directly
// which is break the bidirectional link of free states.
func (m *Matcher) occupyState(state, parentState int) {
	firstFreeState := m.firstFreeState()
	lastFreeState := m.lastFreeState()
	if firstFreeState == lastFreeState {
		m.check[0] = 0
	} else {
		switch state {
		case firstFreeState:
			next := -1 * m.check[state]
			m.check[0] = -1 * next
			m.base[next] = m.base[state]
		case lastFreeState:
			prev := -1 * m.base[state]
			m.base[firstFreeState] = -1 * prev
			m.check[prev] = -1
		default:
			next := -1 * m.check[state]
			prev := -1 * m.base[state]
			m.check[prev] = -1 * next
			m.base[next] = -1 * prev
		}
	}
	m.check[state] = parentState
	m.base[state] = leaf
}

// setFailState sets the output of the fail function for input state. It will
// traverse up the fail states of it's ancestors until it reaches a fail state
// with a transition for offset.
func (m *Matcher) setFailState(state, parentState, offset int) {
	failState := m.fail[parentState]
	for {
		if m.hasEdge(failState, offset) {
			m.fail[state] = m.base[failState] + offset
			break
		}
		if failState == 0 {
			break
		}
		failState = m.fail[failState]
	}
}

// unionFailOutput unions the output function for failState with the output
// function for state and sets the result as the output function for state.
// This allows us to match substrings, commenting out this body would match
// every word that is not a substring.
func (m *Matcher) unionFailOutput(state, failState int) {
	m.output[state] = append([]SWord{}, m.output[failState]...)
}

// findBase finds a base value which has free states in the positions that
// correspond to each edge transition in edges. If this does not exist, then
// base and check (and the fail array for consistency) will be extended just
// enough to fit each transition.
// The extension will maintain the bidirectional link of free states.
func (m *Matcher) findBase(edges []byte) int {
	if len(edges) == 0 {
		return leaf
	}

	min := int(edges[0])
	max := int(edges[len(edges)-1])
	width := max - min
	freeState := m.firstFreeState()
	for freeState != -1 {
		valid := true
		for _, e := range edges[1:] {
			state := freeState + int(e) - min
			if state >= len(m.check) {
				break
			} else if m.check[state] >= 0 {
				valid = false
				break
			}
		}

		if valid {
			if freeState+width >= len(m.check) {
				m.increaseSize(width - len(m.check) + freeState + 1)
			}
			return freeState - min
		}

		freeState = m.nextFreeState(freeState)
	}
	freeState = len(m.check)
	m.increaseSize(width + 1)
	return freeState - min
}

// increaseSize increases the size of base, check, and fail to ensure they
// remain the same size.
// It also sets the default value for these new unoccupied states which form
// bidirectional links to allow fast access to empty states. These
// bidirectional links only pertain to base and check.
//
// Example:
// m:
//
//	base:  [ 5 0 0 ]
//	check: [ 0 0 0 ]
//
// increaseSize(3):
//
//	base:  [ 5  0 0 -5 -3 -4 ]
//	check: [ -3 0 0 -4 -5 -1 ]
//
// increaseSize(3):
//
//	base:  [ 5  0 0 -8 -3 -4 -5 -6 -7]
//	check: [ -3 0 0 -4 -5 -6 -7 -8 -1]
//
// m:
//
//	base:  [ 5 0 0 ]
//	check: [ 0 0 0 ]
//
// increaseSize(1):
//
//	base:  [ 5  0 0 -3 ]
//	check: [ -3 0 0 -1 ]
//
// increaseSize(1):
//
//	base:  [ 5  0 0 -4 -3 ]
//	check: [ -3 0 0 -4 -1 ]
//
// increaseSize(1):
//
//	base:  [ 5  0 0 -5 -3 -4 ]
//	check: [ -3 0 0 -4 -5 -1 ]
func (m *Matcher) increaseSize(dsize int) {
	if dsize == 0 {
		return
	}

	m.base = append(m.base, make([]int, dsize)...)
	m.check = append(m.check, make([]int, dsize)...)
	m.fail = append(m.fail, make([]int, dsize)...)
	m.output = append(m.output, make([][]SWord, dsize)...)

	lastFreeState := m.lastFreeState()
	firstFreeState := m.firstFreeState()
	for i := len(m.check) - dsize; i < len(m.check); i++ {
		if lastFreeState == -1 {
			m.check[0] = -1 * i
			m.base[i] = -1 * i
			m.check[i] = -1
			firstFreeState = i
			lastFreeState = i
		} else {
			m.base[i] = -1 * lastFreeState
			m.check[i] = -1
			m.base[firstFreeState] = -1 * i
			m.check[lastFreeState] = -1 * i
			lastFreeState = i
		}
	}
}

// nextFreeState uses the nature of the bidirectional link to determine the
// closest free state at a larger index. Since the check array holds the
// negative index of the next free state, except for the last free state which
// has a value of -1, negating this value is the next free state.
func (m *Matcher) nextFreeState(curFreeState int) int {
	nextState := -1 * m.check[curFreeState]

	// state 1 can never be a free state.
	if nextState == 1 {
		return -1
	}

	return nextState
}

// firstFreeState uses the first value in the check array which points to the
// first free state. A value of 0 means there are no free states and -1 is
// returned.
func (m *Matcher) firstFreeState() int {
	state := m.check[0]
	if state != 0 {
		return -1 * state
	}
	return -1
}

// lastFreeState uses the base value of the first free state which points the
// last free state.
func (m *Matcher) lastFreeState() int {
	firstFree := m.firstFreeState()
	if firstFree != -1 {
		return -1 * m.base[firstFree]
	}
	return -1
}

// hasEdge determines if the fromState has a transition for offset.
func (m *Matcher) hasEdge(fromState, offset int) bool {
	toState := m.base[fromState] + offset
	return toState > 0 && toState < len(m.check) && m.check[toState] == fromState
}

// Match represents a matched pattern in the text
type Match struct {
	Word  []byte // the matched pattern
	Index int    // the start index of the match
}

type Matches interface {
	Append(key int, position int)
	Count() int
}

func (m *Matcher) findAll(text []byte) []*Match {
	var matches []*Match
	state := 0
	for i, b := range text {
		offset := int(b)
		for state != 0 && !m.hasEdge(state, offset) {
			state = m.fail[state]
		}

		if m.hasEdge(state, offset) {
			state = m.base[state] + offset
		}
		for _, item := range m.output[state] {
			matches = append(matches, &Match{text[i-int(item.Len)+1 : i+1], i - int(item.Len) + 1})
		}
	}
	return matches
}

func (m *Matcher) findAllReader(reader io.Reader, matches Matches) {
	state := 0

	var bchan = make(chan []byte, 1024)
	go func() {
		pool := sync.Pool{
			New: func() any {
				return make([]byte, 512)
			},
		}
		var err error
		var n int
		for err == nil {
			b := pool.New().([]byte)
			n, err = reader.Read(b)
			bchan <- b[:n]
		}
		close(bchan)
	}()

	i := 1
	for bb := range bchan {
		for j := 0; j < len(bb); j++ {
			offset := int(bb[j])
			for state != 0 && !m.hasEdge(state, offset) {
				state = m.fail[state]
			}

			if m.hasEdge(state, offset) {
				state = m.base[state] + offset
			}
			for _, item := range m.output[state] {
				matches.Append(i, int(item.Key))
			}
			i++
		}
	}
}

// FindAllByteSlice finds all instances of the patterns in the text.
func (m *Matcher) FindAllByteSlice(text []byte) (matches []*Match) {
	return m.findAll(text)
}

func (m *Matcher) FindAllByteReader(reader io.Reader, matches Matches) {
	m.findAllReader(reader, matches)
}

// FindAllString finds all instances of the patterns in the text.
func (m *Matcher) FindAllString(text string) []*Match {
	return m.FindAllByteSlice([]byte(text))
}

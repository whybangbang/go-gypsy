// Copyright 2013 Google, Inc.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package yaml

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Parse returns a root-level Node parsed from the lines read from r.  In
// general, this will be done for you by one of the File constructors.
func Parse(r io.Reader) (node Node, err error) {
	lb := &lineBuffer{
		Reader: bufio.NewReader(r),
	}

	defer func() {
		if r := recover(); r != nil {
			switch r := r.(type) {
			case error:
				err = r
			case string:
				err = errors.New(r)
			default:
				err = fmt.Errorf("%v", r)
			}
		}
	}()

	node = parseNode(lb, 0, nil)
	return
}

// Supporting types and constants

const (
	typUnknown = iota
	typSequence
	typMapping
	typScalar
)

var typNames = []string{
	"Unknown", "Sequence", "Mapping", "Scalar",
}

type lineReader interface {
	Next(minIndent int) *indentedLine
}

type indentedLine struct {
	lineno int
	indent int
	line   []byte
}

func (line *indentedLine) String() string {
	return fmt.Sprintf("%2d: %s%s", line.indent,
		strings.Repeat(" ", 0*line.indent), string(line.line))
}

// parseNode 和 inlineValue都会递归调用
// inlineValue 处理行后内容
// parseNode 是处理下几行的内容
func parseNode(r lineReader, ind int, initial Node) (node Node) {
	first := true
	node = initial

	// read lines
	for {
		line := r.Next(ind)
		if line == nil {
			break
		}

		if len(line.line) == 0 {
			continue
		}

		if first {
			ind = line.indent
			first = false
		}

		// 上面这个存的是类型，下面这个存的是附属信息，例如list 下面存的-,map下面存的:前面的
		types := []int{}
		pieces := []string{}

		var inlineValue func([]byte)
		inlineValue = func(partial []byte) {
			// TODO(kevlar): This can be a for loop now
			vtyp, brk := getType(partial)
			// 内容做区分 例如list begin 是- end 是内容
			begin, end := partial[:brk], partial[brk:]

			if vtyp == typMapping {
				end = end[1:]
			}
			end = bytes.TrimLeft(end, " ")

			switch vtyp {
			case typScalar:
				types = append(types, typScalar)
				pieces = append(pieces, string(end))
				return
			case typMapping:
				types = append(types, typMapping)
				pieces = append(pieces, strings.TrimSpace(string(begin)))

				// 如果是 | 则意味着多行，所以继续解析
				trimmed := bytes.TrimSpace(end)
				if len(trimmed) == 1 && trimmed[0] == '|' {
					text := ""

					for {
						l := r.Next(1)
						if l == nil {
							break
						}

						s := string(l.line)
						s = strings.TrimSpace(s)
						if len(s) == 0 {
							break
						}
						text = text + "\n" + s
					}

					types = append(types, typScalar)
					pieces = append(pieces, string(text))
					return
				}
				inlineValue(end)
			case typSequence:
				types = append(types, typSequence)
				pieces = append(pieces, "-")
				inlineValue(end)
			}
		}

		inlineValue(line.line)
		var prev Node

		// Nest inlines
		// 处理一行中的内容
		for len(types) > 0 {
			last := len(types) - 1
			typ, piece := types[last], pieces[last]

			var current Node
			// 如果当前行只有1个类型，则current等于上一行的最后处理的
			// 只有倒退到第一个的时候才会给current设置上值用于和上一行比较
			if last == 0 {
				fmt.Println("run")
				current = node
			}
			//child := parseNode(r, line.indent+1, typUnknown) // TODO allow scalar only

			// Add to current node
			switch typ {
			case typScalar: // last will be == nil
				if _, ok := current.(Scalar); current != nil && !ok {
					panic("cannot append scalar to non-scalar node")
				}
				if piece[0] == '"' && piece[len(piece)-1] == '"'{
					piece = piece[1:len(piece)-1]
				}
				if current != nil {
					current = Scalar(piece) + " " + current.(Scalar)
					break
				}
				// 如果是scalar(string) 则直接把值给设置到里面
				current = Scalar(piece)
			case typMapping:
				var mapNode Map
				var ok bool
				var child Node

				// Get the current map, if there is one
				if mapNode, ok = current.(Map); current != nil && !ok {
					fmt.Println("map panic")
					_ = current.(Map) // panic
				} else if current == nil {
					mapNode = make(Map)
				}

				if _, inlineMap := prev.(Scalar); inlineMap && last > 0 {
					current = Map{
						piece: prev,
					}
					break
				}

				//递归调用,这个方法会把接下来里面的几层都算到这个node上
				child = parseNode(r, line.indent+1, prev)
				mapNode[piece] = child
				current = mapNode

			case typSequence:
				var listNode List
				var ok bool
				var child Node

				// Get the current list, if there is one
				if listNode, ok = current.(List); current != nil && !ok {
					_ = current.(List) // panic
				} else if current == nil {
					listNode = make(List, 0)
				}

				if _, inlineList := prev.(Scalar); inlineList && last > 0 {
					current = List{
						prev,
					}
					break
				}

				child = parseNode(r, line.indent+1, prev)
				listNode = append(listNode, child)
				current = listNode

			}

			if last < 0 {
				last = 0
			}
			types = types[:last]
			pieces = pieces[:last]
			prev = current
		}

		node = prev
	}
	return
}

// split 是分割点，例如list就是 - 的位置
func getType(line []byte) (typ, split int) {
	if len(line) == 0 {
		return
	}

	if line[0] == '-' {
		// 说明是list
		fmt.Println("list")
		typ = typSequence
		split = 1
		return
	}

	typ = typScalar

	if line[0] == ' ' || line[0] == '"' {
		return
	}

	// the first character is real
	// need to iterate past the first word
	// things like "foo:" and "foo :" are mappings
	// everything else is a scalar
	// 是否" ", "\"", ":" 又一个存在
	idx := bytes.IndexAny(line, " \":")
	// 不存在直接判断为 typScalar
	if idx < 0 {
		return
	}

	// 如果是"\"" 则 也为 typScalar
	if line[idx] == '"' {
		return
	}

	// 如果是":" 则为mapping
	if line[idx] == ':' {
		typ = typMapping
		split = idx
	} else if line[idx] == ' ' {
		// we have a space
		// need to see if its all spaces until a :
		// 如果是空格就一直往后找
		for i := idx; i < len(line); i++ {
			switch ch := line[i]; ch {
			case ' ':
				continue
			case ':':
				//: 而且后面必须是空格才能算作是typMapping, 而且只检测第一个，按理说没问题
				// only split on colons followed by a space
				if i+1 < len(line) && line[i+1] != ' ' {
					continue
				}

				typ = typMapping
				split = i
				break
			default:
				break
			}
		}
	}

	if typ == typMapping && split+1 < len(line) && line[split+1] != ' ' {
		typ = typScalar
		split = 0
	}

	return
}

// lineReader implementations

type lineBuffer struct {
	*bufio.Reader
	readLines int
	pending   *indentedLine
}

func (lb *lineBuffer) Next(min int) (next *indentedLine) {
	if lb.pending == nil {
		var (
			read []byte
			more bool
			err  error
		)

		l := new(indentedLine)
		l.lineno = lb.readLines
		more = true
		for more {
			read, more, err = lb.ReadLine()
			if err != nil {
				if err == io.EOF {
					return nil
				}
				panic(err)
			}
			l.line = append(l.line, read...)
		}
		lb.readLines++

		for _, ch := range l.line {
			switch ch {
			case ' ':
				l.indent += 1
				continue
			default:
			}
			break
		}
		l.line = l.line[l.indent:]

		// Ignore blank lines and comments.
		if len(l.line) == 0 || l.line[0] == '#' || string(l.line) == "---"{
			return lb.Next(min)
		}

		lb.pending = l
	}
	next = lb.pending
	if next.indent < min {
		return nil
	}
	lb.pending = nil
	return
}

type lineSlice []*indentedLine

func (ls *lineSlice) Next(min int) (next *indentedLine) {
	if len(*ls) == 0 {
		return nil
	}
	next = (*ls)[0]
	if next.indent < min {
		return nil
	}
	*ls = (*ls)[1:]
	return
}

func (ls *lineSlice) Push(line *indentedLine) {
	*ls = append(*ls, line)
}

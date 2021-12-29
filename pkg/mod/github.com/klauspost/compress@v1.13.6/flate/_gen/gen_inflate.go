//go:build generate
// +build generate

//go:generate go run $GOFILE && gofmt -w ../inflate_gen.go

package main

import (
	"os"
	"strings"
)

func main() {
	f, err := os.Create("../inflate_gen.go")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	types := []string{"*bytes.Buffer", "*bytes.Reader", "*bufio.Reader", "*strings.Reader"}
	names := []string{"BytesBuffer", "BytesReader", "BufioReader", "StringsReader"}
	imports := []string{"bytes", "bufio", "io", "strings", "math/bits"}
	f.WriteString(`// Code generated by go generate gen_inflate.go. DO NOT EDIT.

package flate

import (
`)

	for _, imp := range imports {
		f.WriteString("\t\"" + imp + "\"\n")
	}
	f.WriteString(")\n\n")

	template := `

// Decode a single Huffman block from f.
// hl and hd are the Huffman states for the lit/length values
// and the distance values, respectively. If hd == nil, using the
// fixed distance encoding associated with fixed Huffman blocks.
func (f *decompressor) $FUNCNAME$() {
	const (
		stateInit = iota // Zero value must be stateInit
		stateDict
	)
	fr := f.r.($TYPE$)

	switch f.stepState {
	case stateInit:
		goto readLiteral
	case stateDict:
		goto copyHistory
	}

readLiteral:
	// Read literal and/or (length, distance) according to RFC section 3.2.3.
	{
		var v int
		{
			// Inlined v, err := f.huffSym(f.hl)
			// Since a huffmanDecoder can be empty or be composed of a degenerate tree
			// with single element, huffSym must error on these two edge cases. In both
			// cases, the chunks slice will be 0 for the invalid sequence, leading it
			// satisfy the n == 0 check below.
			n := uint(f.hl.maxRead)
			// Optimization. Compiler isn't smart enough to keep f.b,f.nb in registers,
			// but is smart enough to keep local variables in registers, so use nb and b,
			// inline call to moreBits and reassign b,nb back to f on return.
			nb, b := f.nb, f.b
			for {
				for nb < n {
					c, err := fr.ReadByte()
					if err != nil {
						f.b = b
						f.nb = nb
						f.err = noEOF(err)
						return
					}
					f.roffset++
					b |= uint32(c) << (nb & regSizeMaskUint32)
					nb += 8
				}
				chunk := f.hl.chunks[b&(huffmanNumChunks-1)]
				n = uint(chunk & huffmanCountMask)
				if n > huffmanChunkBits {
					chunk = f.hl.links[chunk>>huffmanValueShift][(b>>huffmanChunkBits)&f.hl.linkMask]
					n = uint(chunk & huffmanCountMask)
				}
				if n <= nb {
					if n == 0 {
						f.b = b
						f.nb = nb
						if debugDecode {
							fmt.Println("huffsym: n==0")
						}
						f.err = CorruptInputError(f.roffset)
						return
					}
					f.b = b >> (n & regSizeMaskUint32)
					f.nb = nb - n
					v = int(chunk >> huffmanValueShift)
					break
				}
			}
		}

		var length int
		switch {
		case v < 256:
			f.dict.writeByte(byte(v))
			if f.dict.availWrite() == 0 {
				f.toRead = f.dict.readFlush()
				f.step = (*decompressor).$FUNCNAME$
				f.stepState = stateInit
				return
			}
			goto readLiteral
		case v == 256:
			f.finishBlock()
			return
		// otherwise, reference to older data
		case v < 265:
			length = v - (257 - 3)
		case v < maxNumLit:
			val := decCodeToLen[(v - 257)]
			length = int(val.length) + 3
			n := uint(val.extra)
			for f.nb < n {
				c, err := fr.ReadByte()
				if err != nil {
					if debugDecode {
						fmt.Println("morebits n>0:", err)
					}
					f.err = err
					return
				}
				f.roffset++
				f.b |= uint32(c) << f.nb
				f.nb += 8	
			}
			length += int(f.b & uint32(1<<(n&regSizeMaskUint32)-1))
			f.b >>= n & regSizeMaskUint32
			f.nb -= n
		default:
			if debugDecode {
				fmt.Println(v, ">= maxNumLit")
			}
			f.err = CorruptInputError(f.roffset)
			return
		}

		var dist uint32
		if f.hd == nil {
			for f.nb < 5 {
				c, err := fr.ReadByte()
				if err != nil {
					if debugDecode {
						fmt.Println("morebits f.nb<5:", err)
					}
					f.err = err
					return
				}
				f.roffset++
				f.b |= uint32(c) << f.nb
				f.nb += 8
			}
			dist = uint32(bits.Reverse8(uint8(f.b & 0x1F << 3)))
			f.b >>= 5
			f.nb -= 5
		} else {
			// Since a huffmanDecoder can be empty or be composed of a degenerate tree
			// with single element, huffSym must error on these two edge cases. In both
			// cases, the chunks slice will be 0 for the invalid sequence, leading it
			// satisfy the n == 0 check below.
			n := uint(f.hd.maxRead)
			// Optimization. Compiler isn't smart enough to keep f.b,f.nb in registers,
			// but is smart enough to keep local variables in registers, so use nb and b,
			// inline call to moreBits and reassign b,nb back to f on return.
			nb, b := f.nb, f.b
			for {
				for nb < n {
					c, err := fr.ReadByte()
					if err != nil {
						f.b = b
						f.nb = nb
						f.err = noEOF(err)
						return
					}
					f.roffset++
					b |= uint32(c) << (nb & regSizeMaskUint32)
					nb += 8
				}
				chunk := f.hd.chunks[b&(huffmanNumChunks-1)]
				n = uint(chunk & huffmanCountMask)
				if n > huffmanChunkBits {
					chunk = f.hd.links[chunk>>huffmanValueShift][(b>>huffmanChunkBits)&f.hd.linkMask]
					n = uint(chunk & huffmanCountMask)
				}
				if n <= nb {
					if n == 0 {
						f.b = b
						f.nb = nb
						if debugDecode {
							fmt.Println("huffsym: n==0")
						}
						f.err = CorruptInputError(f.roffset)
						return
					}
					f.b = b >> (n & regSizeMaskUint32)
					f.nb = nb - n
					dist = uint32(chunk >> huffmanValueShift)
					break
				}
			}
		}

		switch {
		case dist < 4:
			dist++
		case dist < maxNumDist:
			nb := uint(dist-2) >> 1
			// have 1 bit in bottom of dist, need nb more.
			extra := (dist & 1) << (nb & regSizeMaskUint32)
			for f.nb < nb {
				c, err := fr.ReadByte()
				if err != nil {
					if debugDecode {
						fmt.Println("morebits f.nb<nb:", err)
					}
					f.err = err
					return
				}
				f.roffset++
				f.b |= uint32(c) << f.nb
				f.nb += 8
			}
			extra |= f.b & uint32(1<<(nb&regSizeMaskUint32)-1)
			f.b >>= nb & regSizeMaskUint32
			f.nb -= nb
			dist = 1<<((nb+1)&regSizeMaskUint32) + 1 + extra
		default:
			if debugDecode {
				fmt.Println("dist too big:", dist, maxNumDist)
			}
			f.err = CorruptInputError(f.roffset)
			return
		}

		// No check on length; encoding can be prescient.
		if dist > uint32(f.dict.histSize()) {
			if debugDecode {
				fmt.Println("dist > f.dict.histSize():", dist, f.dict.histSize())
			}
			f.err = CorruptInputError(f.roffset)
			return
		}

		f.copyLen, f.copyDist = length, int(dist)
		goto copyHistory
	}

copyHistory:
	// Perform a backwards copy according to RFC section 3.2.3.
	{
		cnt := f.dict.tryWriteCopy(f.copyDist, f.copyLen)
		if cnt == 0 {
			cnt = f.dict.writeCopy(f.copyDist, f.copyLen)
		}
		f.copyLen -= cnt

		if f.dict.availWrite() == 0 || f.copyLen > 0 {
			f.toRead = f.dict.readFlush()
			f.step = (*decompressor).$FUNCNAME$ // We need to continue this work
			f.stepState = stateDict
			return
		}
		goto readLiteral
	}
}

`
	for i, t := range types {
		s := strings.Replace(template, "$FUNCNAME$", "huffman"+names[i], -1)
		s = strings.Replace(s, "$TYPE$", t, -1)
		f.WriteString(s)
	}
	f.WriteString("func (f *decompressor) huffmanBlockDecoder() func() {\n")
	f.WriteString("\tswitch f.r.(type) {\n")
	for i, t := range types {
		f.WriteString("\t\tcase " + t + ":\n")
		f.WriteString("\t\t\treturn f.huffman" + names[i] + "\n")
	}
	f.WriteString("\t\tdefault:\n")
	f.WriteString("\t\t\treturn f.huffmanBlockGeneric")
	f.WriteString("\t}\n}\n")
}

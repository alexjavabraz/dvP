package main

// Decodificacao de nomes de funcoes e eventos.
// Assinaturas conhecidas (ERC-20 etc.) vem embutidas; ABIs extras podem ser carregadas
// com --abi-dir (arquivos .json com a ABI ou artefatos Foundry/Hardhat com campo "abi").
// E o ponto de extensao para as telas de CCB e Cestas.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
)

type abiParam struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Indexed    bool       `json:"indexed"`
	Components []abiParam `json:"components"`
}

type abiEntry struct {
	Type   string     `json:"type"`
	Name   string     `json:"name"`
	Inputs []abiParam `json:"inputs"`
}

type sigInfo struct {
	Name   string
	Sig    string
	Inputs []abiParam
}

// Decoder mapeia seletor de 4 bytes -> funcao e topic0 -> evento.
type Decoder struct {
	funcs  map[[4]byte]sigInfo
	events map[Hash][]sigInfo // ERC-20 e ERC-721 compartilham o topic0 de Transfer
	files  []string
}

var builtinFuncs = []string{
	"transfer(address to,uint256 amount)",
	"transferFrom(address from,address to,uint256 amount)",
	"approve(address spender,uint256 amount)",
	"increaseAllowance(address spender,uint256 addedValue)",
	"decreaseAllowance(address spender,uint256 subtractedValue)",
	"mint(address to,uint256 amount)",
	"burn(uint256 amount)",
	"burnFrom(address account,uint256 amount)",
	"pause()", "unpause()",
	"transferOwnership(address newOwner)", "renounceOwnership()",
	"grantRole(bytes32 role,address account)", "revokeRole(bytes32 role,address account)",
}

var builtinEvents = []string{
	"Transfer(address indexed from,address indexed to,uint256 value)",
	"Approval(address indexed owner,address indexed spender,uint256 value)",
	"OwnershipTransferred(address indexed previousOwner,address indexed newOwner)",
	"Paused(address account)", "Unpaused(address account)",
	"RoleGranted(bytes32 indexed role,address indexed account,address indexed sender)",
	"RoleRevoked(bytes32 indexed role,address indexed account,address indexed sender)",
}

func NewDecoder() *Decoder {
	d := &Decoder{funcs: map[[4]byte]sigInfo{}, events: map[Hash][]sigInfo{}}
	for _, s := range builtinFuncs {
		d.addHuman("function", s)
	}
	for _, s := range builtinEvents {
		d.addHuman("event", s)
	}
	return d
}

// addHuman aceita "nome(tipo nome,tipo indexed nome)".
func (d *Decoder) addHuman(kind, s string) {
	open := strings.Index(s, "(")
	name := s[:open]
	inner := strings.TrimSuffix(s[open+1:], ")")
	var params []abiParam
	if strings.TrimSpace(inner) != "" {
		for _, p := range strings.Split(inner, ",") {
			f := strings.Fields(p)
			ap := abiParam{Type: f[0]}
			for _, x := range f[1:] {
				if x == "indexed" {
					ap.Indexed = true
				} else {
					ap.Name = x
				}
			}
			params = append(params, ap)
		}
	}
	d.add(abiEntry{Type: kind, Name: name, Inputs: params})
}

func canonicalType(p abiParam) string {
	if strings.HasPrefix(p.Type, "tuple") {
		var parts []string
		for _, c := range p.Components {
			parts = append(parts, canonicalType(c))
		}
		return "(" + strings.Join(parts, ",") + ")" + strings.TrimPrefix(p.Type, "tuple")
	}
	return p.Type
}

func (d *Decoder) add(e abiEntry) {
	var types []string
	for _, in := range e.Inputs {
		types = append(types, canonicalType(in))
	}
	sig := e.Name + "(" + strings.Join(types, ",") + ")"
	h := Keccak256([]byte(sig))
	info := sigInfo{Name: e.Name, Sig: sig, Inputs: e.Inputs}
	switch e.Type {
	case "function":
		var sel [4]byte
		copy(sel[:], h[:4])
		d.funcs[sel] = info
	case "event":
		key := Hash(h)
		for i, ex := range d.events[key] {
			if indexedCount(ex.Inputs) == indexedCount(info.Inputs) {
				d.events[key][i] = info // mesma forma: a ABI carregada depois prevalece
				return
			}
		}
		d.events[key] = append(d.events[key], info)
	}
}

// LoadDir carrega todos os .json de um diretorio.
func (d *Decoder) LoadDir(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var entries []abiEntry
		if json.Unmarshal(raw, &entries) != nil {
			var art struct {
				ABI []abiEntry `json:"abi"`
			}
			if err := json.Unmarshal(raw, &art); err != nil || art.ABI == nil {
				return fmt.Errorf("%s: nao e uma ABI JSON", f)
			}
			entries = art.ABI
		}
		for _, e := range entries {
			if e.Type == "function" || e.Type == "event" {
				d.add(e)
			}
		}
		d.files = append(d.files, filepath.Base(f))
	}
	return nil
}

// ------------------------------------------------------------- decodificacao

type DecodedArg struct {
	Name  string
	Type  string
	Value string
}

type Decoded struct {
	Name string
	Sig  string
	Args []DecodedArg
}

func (d *Decoder) Method(input []byte) (Decoded, bool) {
	if len(input) < 4 {
		return Decoded{}, false
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	info, ok := d.funcs[sel]
	if !ok {
		return Decoded{}, false
	}
	return Decoded{Name: info.Name, Sig: info.Sig, Args: decodeParams(info.Inputs, input[4:])}, true
}

func (d *Decoder) Event(l Log) (Decoded, bool) {
	if len(l.Topics) == 0 {
		return Decoded{}, false
	}
	cands, ok := d.events[l.Topics[0]]
	if !ok || len(cands) == 0 {
		return Decoded{}, false
	}
	// escolhe a variante cujo numero de campos indexados bate com os topics do log
	info, found := cands[0], false
	for _, c := range cands {
		if indexedCount(c.Inputs) == len(l.Topics)-1 {
			info, found = c, true
			break
		}
	}
	if !found {
		return Decoded{Name: info.Name, Sig: info.Sig}, true
	}
	var nonIdx []abiParam
	for _, in := range info.Inputs {
		if !in.Indexed {
			nonIdx = append(nonIdx, in)
		}
	}
	dataArgs := decodeParams(nonIdx, l.Data)
	out := Decoded{Name: info.Name, Sig: info.Sig}
	ti, di := 1, 0
	for _, in := range info.Inputs {
		if in.Indexed {
			out.Args = append(out.Args, DecodedArg{in.Name, in.Type, decodeWord(in.Type, l.Topics[ti][:], true)})
			ti++
		} else if di < len(dataArgs) {
			out.Args = append(out.Args, dataArgs[di])
			di++
		}
	}
	return out, true
}

func indexedCount(ps []abiParam) int {
	n := 0
	for _, p := range ps {
		if p.Indexed {
			n++
		}
	}
	return n
}

// decodeParams decodifica tipos estaticos e os dinamicos mais comuns (string, bytes, T[]).
func decodeParams(params []abiParam, data []byte) []DecodedArg {
	var out []DecodedArg
	for i, p := range params {
		off := i * 32
		if off+32 > len(data) {
			out = append(out, DecodedArg{p.Name, p.Type, "(dados insuficientes)"})
			continue
		}
		word := data[off : off+32]
		out = append(out, DecodedArg{p.Name, p.Type, decodeValue(p, word, data)})
	}
	return out
}

func decodeValue(p abiParam, word, data []byte) string {
	t := p.Type
	switch {
	case t == "string" || t == "bytes":
		o := new(big.Int).SetBytes(word).Uint64()
		if o+32 > uint64(len(data)) {
			return "(invalido)"
		}
		l := new(big.Int).SetBytes(data[o : o+32]).Uint64()
		if o+32+l > uint64(len(data)) {
			return "(invalido)"
		}
		v := data[o+32 : o+32+l]
		if t == "string" {
			return fmt.Sprintf("%q", string(v))
		}
		return "0x" + hex.EncodeToString(v)
	case strings.HasSuffix(t, "[]") && !strings.HasPrefix(t, "tuple"):
		elem := strings.TrimSuffix(t, "[]")
		o := new(big.Int).SetBytes(word).Uint64()
		if o+32 > uint64(len(data)) {
			return "(invalido)"
		}
		n := new(big.Int).SetBytes(data[o : o+32]).Uint64()
		if n > 1000 || o+32+n*32 > uint64(len(data)) {
			return "(array longo)"
		}
		var parts []string
		for i := uint64(0); i < n; i++ {
			s := o + 32 + i*32
			parts = append(parts, decodeWord(elem, data[s:s+32], false))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case strings.HasPrefix(t, "tuple"):
		return "(tupla)"
	default:
		return decodeWord(t, word, false)
	}
}

func decodeWord(t string, w []byte, indexedTopic bool) string {
	switch {
	case t == "address":
		var a Address
		copy(a[:], w[12:])
		return a.Hex()
	case t == "bool":
		if new(big.Int).SetBytes(w).Sign() != 0 {
			return "true"
		}
		return "false"
	case strings.HasPrefix(t, "uint"):
		return new(big.Int).SetBytes(w).String()
	case strings.HasPrefix(t, "int"):
		v := new(big.Int).SetBytes(w)
		if len(w) == 32 && w[0]&0x80 != 0 {
			v.Sub(v, new(big.Int).Lsh(big.NewInt(1), 256))
		}
		return v.String()
	case indexedTopic:
		return "0x" + hex.EncodeToString(w) // tipos dinamicos indexados viram hash
	default:
		return "0x" + hex.EncodeToString(w)
	}
}

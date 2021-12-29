package vm

import (
	"crypto/elliptic"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"math/big"
	"os"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/nspcc-dev/neo-go/pkg/core/interop/interopnames"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	"github.com/nspcc-dev/neo-go/pkg/encoding/bigint"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/callflag"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/nef"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/trigger"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/util/slice"
	"github.com/nspcc-dev/neo-go/pkg/vm/opcode"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
)

type errorAtInstruct struct {
	ip  int
	op  opcode.Opcode
	err interface{}
}

func (e *errorAtInstruct) Error() string {
	return fmt.Sprintf("at instruction %d (%s): %s", e.ip, e.op, e.err)
}

func newError(ip int, op opcode.Opcode, err interface{}) *errorAtInstruct {
	return &errorAtInstruct{ip: ip, op: op, err: err}
}

// StateMessage is a vm state message which could be used as additional info for example by cli.
type StateMessage string

const (
	// MaxInvocationStackSize is the maximum size of an invocation stack.
	MaxInvocationStackSize = 1024

	// MaxTryNestingDepth is the maximum level of TRY nesting allowed,
	// that is you can't have more exception handling contexts than this.
	MaxTryNestingDepth = 16

	// MaxStackSize is the maximum number of items allowed to be
	// on all stacks at once.
	MaxStackSize = 2 * 1024

	maxSHLArg = stackitem.MaxBigIntegerSizeBits
)

// SyscallHandler is a type for syscall handler.
type SyscallHandler = func(*VM, uint32) error

// VM represents the virtual machine.
type VM struct {
	state State

	// callback to get interop price
	getPrice func(opcode.Opcode, []byte) int64

	istack Stack  // invocation stack.
	estack *Stack // execution stack.

	uncaughtException stackitem.Item // exception being handled

	refs refCounter

	gasConsumed int64
	GasLimit    int64

	// SyscallHandler handles SYSCALL opcode.
	SyscallHandler func(v *VM, id uint32) error

	// LoadToken handles CALLT opcode.
	LoadToken func(id int32) error

	trigger trigger.Type

	// invTree is a top-level invocation tree (if enabled).
	invTree *InvocationTree
}

var bigOne = big.NewInt(1)

// New returns a new VM object ready to load AVM bytecode scripts.
func New() *VM {
	return NewWithTrigger(trigger.Application)
}

// NewWithTrigger returns a new VM for executions triggered by t.
func NewWithTrigger(t trigger.Type) *VM {
	vm := &VM{
		state:   NoneState,
		trigger: t,

		SyscallHandler: defaultSyscallHandler,
	}

	initStack(&vm.istack, "invocation", nil)
	vm.istack.elems = make([]Element, 0, 8) // Most of invocations use one-two contracts, but they're likely to have internal calls.
	vm.estack = newStack("evaluation", &vm.refs)
	return vm
}

// SetPriceGetter registers the given PriceGetterFunc in v.
// f accepts vm's Context, current instruction and instruction parameter.
func (v *VM) SetPriceGetter(f func(opcode.Opcode, []byte) int64) {
	v.getPrice = f
}

// GasConsumed returns the amount of GAS consumed during execution.
func (v *VM) GasConsumed() int64 {
	return v.gasConsumed
}

// AddGas consumes specified amount of gas. It returns true iff gas limit wasn't exceeded.
func (v *VM) AddGas(gas int64) bool {
	v.gasConsumed += gas
	return v.GasLimit < 0 || v.gasConsumed <= v.GasLimit
}

// Estack returns the evaluation stack so interop hooks can utilize this.
func (v *VM) Estack() *Stack {
	return v.estack
}

// Istack returns the invocation stack so interop hooks can utilize this.
func (v *VM) Istack() *Stack {
	return &v.istack
}

// PrintOps prints the opcodes of the current loaded program to stdout.
func (v *VM) PrintOps(out io.Writer) {
	if out == nil {
		out = os.Stdout
	}
	w := tabwriter.NewWriter(out, 0, 0, 4, ' ', 0)
	fmt.Fprintln(w, "INDEX\tOPCODE\tPARAMETER\t")
	realctx := v.Context()
	ctx := realctx.Copy()
	ctx.ip = 0
	ctx.nextip = 0
	for {
		cursor := ""
		instr, parameter, err := ctx.Next()
		if ctx.ip == realctx.ip {
			cursor = "<<"
		}
		if err != nil {
			fmt.Fprintf(w, "%d\t%s\tERROR: %s\t%s\n", ctx.ip, instr, err, cursor)
			break
		}
		var desc = ""
		if parameter != nil {
			switch instr {
			case opcode.JMP, opcode.JMPIF, opcode.JMPIFNOT, opcode.CALL,
				opcode.JMPEQ, opcode.JMPNE,
				opcode.JMPGT, opcode.JMPGE, opcode.JMPLE, opcode.JMPLT,
				opcode.JMPL, opcode.JMPIFL, opcode.JMPIFNOTL, opcode.CALLL,
				opcode.JMPEQL, opcode.JMPNEL,
				opcode.JMPGTL, opcode.JMPGEL, opcode.JMPLEL, opcode.JMPLTL,
				opcode.PUSHA, opcode.ENDTRY, opcode.ENDTRYL:
				desc = getOffsetDesc(ctx, parameter)
			case opcode.TRY, opcode.TRYL:
				catchP, finallyP := getTryParams(instr, parameter)
				desc = fmt.Sprintf("catch %s, finally %s",
					getOffsetDesc(ctx, catchP), getOffsetDesc(ctx, finallyP))
			case opcode.INITSSLOT:
				desc = fmt.Sprint(parameter[0])
			case opcode.CONVERT, opcode.ISTYPE:
				typ := stackitem.Type(parameter[0])
				desc = fmt.Sprintf("%s (%x)", typ, parameter[0])
			case opcode.INITSLOT:
				desc = fmt.Sprintf("%d local, %d arg", parameter[0], parameter[1])
			case opcode.SYSCALL:
				name, err := interopnames.FromID(GetInteropID(parameter))
				if err != nil {
					name = "not found"
				}
				desc = fmt.Sprintf("%s (%x)", name, parameter)
			case opcode.PUSHINT8, opcode.PUSHINT16, opcode.PUSHINT32,
				opcode.PUSHINT64, opcode.PUSHINT128, opcode.PUSHINT256:
				val := bigint.FromBytes(parameter)
				desc = fmt.Sprintf("%d (%x)", val, parameter)
			case opcode.LDLOC, opcode.STLOC, opcode.LDARG, opcode.STARG, opcode.LDSFLD, opcode.STSFLD:
				desc = fmt.Sprintf("%d (%x)", parameter[0], parameter)
			default:
				if utf8.Valid(parameter) {
					desc = fmt.Sprintf("%x (%q)", parameter, parameter)
				} else {
					desc = fmt.Sprintf("%x", parameter)
				}
			}
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", ctx.ip, instr, desc, cursor)
		if ctx.nextip >= len(ctx.prog) {
			break
		}
	}
	w.Flush()
}

func getOffsetDesc(ctx *Context, parameter []byte) string {
	offset, rOffset, err := calcJumpOffset(ctx, parameter)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err)
	}
	return fmt.Sprintf("%d (%d/%x)", offset, rOffset, parameter)
}

// AddBreakPoint adds a breakpoint to the current context.
func (v *VM) AddBreakPoint(n int) {
	ctx := v.Context()
	ctx.breakPoints = append(ctx.breakPoints, n)
}

// AddBreakPointRel adds a breakpoint relative to the current
// instruction pointer.
func (v *VM) AddBreakPointRel(n int) {
	ctx := v.Context()
	v.AddBreakPoint(ctx.nextip + n)
}

// LoadFileWithFlags loads a program in NEF format from the given path, ready to execute it.
func (v *VM) LoadFileWithFlags(path string, f callflag.CallFlag) error {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	nef, err := nef.FileFromBytes(b)
	if err != nil {
		return err
	}
	v.Load(nef.Script)
	return nil
}

// CollectInvocationTree enables collecting invocation tree data.
func (v *VM) EnableInvocationTree() {
	v.invTree = &InvocationTree{}
}

// GetInvocationTree returns current invocation tree structure.
func (v *VM) GetInvocationTree() *InvocationTree {
	return v.invTree
}

// Load initializes the VM with the program given.
func (v *VM) Load(prog []byte) {
	v.LoadWithFlags(prog, callflag.NoneFlag)
}

// LoadWithFlags initializes the VM with the program and flags given.
func (v *VM) LoadWithFlags(prog []byte, f callflag.CallFlag) {
	// Clear all stacks and state, it could be a reload.
	v.istack.Clear()
	v.estack.Clear()
	v.state = NoneState
	v.gasConsumed = 0
	v.invTree = nil
	v.LoadScriptWithFlags(prog, f)
}

// LoadScript loads a script from the internal script table. It
// will immediately push a new context created from this script to
// the invocation stack and starts executing it.
func (v *VM) LoadScript(b []byte) {
	v.LoadScriptWithFlags(b, callflag.NoneFlag)
}

// LoadScriptWithFlags loads script and sets call flag to f.
func (v *VM) LoadScriptWithFlags(b []byte, f callflag.CallFlag) {
	v.loadScriptWithCallingHash(b, nil, v.GetCurrentScriptHash(), util.Uint160{}, f, -1, 0)
}

// LoadScriptWithHash if similar to the LoadScriptWithFlags method, but it also loads
// given script hash directly into the Context to avoid its recalculations and to make
// is possible to override it for deployed contracts with special hashes (the function
// assumes that it is used for deployed contracts setting context's parameters
// accordingly). It's up to user of this function to make sure the script and hash match
// each other.
func (v *VM) LoadScriptWithHash(b []byte, hash util.Uint160, f callflag.CallFlag) {
	v.loadScriptWithCallingHash(b, nil, v.GetCurrentScriptHash(), hash, f, 1, 0)
}

// LoadNEFMethod allows to create a context to execute a method from the NEF
// file with specified caller and executing hash, call flags, return value,
// method and _initialize offsets.
func (v *VM) LoadNEFMethod(exe *nef.File, caller util.Uint160, hash util.Uint160, f callflag.CallFlag,
	hasReturn bool, methodOff int, initOff int) {
	var rvcount int
	if hasReturn {
		rvcount = 1
	}
	v.loadScriptWithCallingHash(exe.Script, exe, caller, hash, f, rvcount, methodOff)
	if initOff >= 0 {
		v.Call(initOff)
	}
}

// loadScriptWithCallingHash is similar to LoadScriptWithHash but sets calling hash explicitly.
// It should be used for calling from native contracts.
func (v *VM) loadScriptWithCallingHash(b []byte, exe *nef.File, caller util.Uint160,
	hash util.Uint160, f callflag.CallFlag, rvcount int, offset int) {
	var sl slot

	v.checkInvocationStackSize()
	ctx := NewContextWithParams(b, rvcount, offset)
	if rvcount != -1 || v.estack.Len() != 0 {
		v.estack = newStack("evaluation", &v.refs)
	}
	ctx.estack = v.estack
	initStack(&ctx.tryStack, "exception", nil)
	ctx.callFlag = f
	ctx.static = &sl
	ctx.scriptHash = hash
	ctx.callingScriptHash = caller
	ctx.NEF = exe
	if v.invTree != nil {
		curTree := v.invTree
		parent := v.Context()
		if parent != nil {
			curTree = parent.invTree
		}
		newTree := &InvocationTree{Current: ctx.ScriptHash()}
		curTree.Calls = append(curTree.Calls, newTree)
		ctx.invTree = newTree
	}
	v.istack.PushItem(ctx)
}

// Context returns the current executed context. Nil if there is no context,
// which implies no program is loaded.
func (v *VM) Context() *Context {
	if v.istack.Len() == 0 {
		return nil
	}
	return v.istack.Peek(0).value.(*Context)
}

// PopResult is used to pop the first item of the evaluation stack. This allows
// us to test compiler and vm in a bi-directional way.
func (v *VM) PopResult() interface{} {
	if v.estack.Len() == 0 {
		return nil
	}
	return v.estack.Pop().Value()
}

// DumpIStack returns json formatted representation of the invocation stack.
func (v *VM) DumpIStack() string {
	return dumpStack(&v.istack)
}

// DumpEStack returns json formatted representation of the execution stack.
func (v *VM) DumpEStack() string {
	return dumpStack(v.estack)
}

// dumpStack returns json formatted representation of the given stack.
func dumpStack(s *Stack) string {
	b, _ := json.MarshalIndent(s, "", "    ")
	return string(b)
}

// State returns the state for the VM.
func (v *VM) State() State {
	return v.state
}

// Ready returns true if the VM ready to execute the loaded program.
// Will return false if no program is loaded.
func (v *VM) Ready() bool {
	return v.istack.Len() > 0
}

// Run starts the execution of the loaded program.
func (v *VM) Run() error {
	var ctx *Context

	if !v.Ready() {
		v.state = FaultState
		return errors.New("no program loaded")
	}

	if v.state.HasFlag(FaultState) {
		// VM already ran something and failed, in general its state is
		// undefined in this case so we can't run anything.
		return errors.New("VM has failed")
	}
	// HaltState (the default) or BreakState are safe to continue.
	v.state = NoneState
	ctx = v.Context()
	for {
		switch {
		case v.state.HasFlag(FaultState):
			// Should be caught and reported already by the v.Step(),
			// but we're checking here anyway just in case.
			return errors.New("VM has failed")
		case v.state.HasFlag(HaltState), v.state.HasFlag(BreakState):
			// Normal exit from this loop.
			return nil
		case v.state == NoneState:
			if err := v.step(ctx); err != nil {
				return err
			}
		default:
			v.state = FaultState
			return errors.New("unknown state")
		}
		// check for breakpoint before executing the next instruction
		ctx = v.Context()
		if ctx != nil && ctx.atBreakPoint() {
			v.state = BreakState
		}
	}
}

// Step 1 instruction in the program.
func (v *VM) Step() error {
	ctx := v.Context()
	return v.step(ctx)
}

// step executes one instruction in given context.
func (v *VM) step(ctx *Context) error {
	op, param, err := ctx.Next()
	if err != nil {
		v.state = FaultState
		return newError(ctx.ip, op, err)
	}
	return v.execute(ctx, op, param)
}

// StepInto behaves the same as “step over” in case if the line does not contain a function. Otherwise
// the debugger will enter the called function and continue line-by-line debugging there.
func (v *VM) StepInto() error {
	ctx := v.Context()

	if ctx == nil {
		v.state = HaltState
	}

	if v.HasStopped() {
		return nil
	}

	if ctx != nil && ctx.prog != nil {
		op, param, err := ctx.Next()
		if err != nil {
			v.state = FaultState
			return newError(ctx.ip, op, err)
		}
		vErr := v.execute(ctx, op, param)
		if vErr != nil {
			return vErr
		}
	}

	cctx := v.Context()
	if cctx != nil && cctx.atBreakPoint() {
		v.state = BreakState
	}
	return nil
}

// StepOut takes the debugger to the line where the current function was called.
func (v *VM) StepOut() error {
	var err error
	if v.state == BreakState {
		v.state = NoneState
	}

	expSize := v.istack.Len()
	for v.state == NoneState && v.istack.Len() >= expSize {
		err = v.StepInto()
	}
	if v.state == NoneState {
		v.state = BreakState
	}
	return err
}

// StepOver takes the debugger to the line that will step over a given line.
// If the line contains a function the function will be executed and the result returned without debugging each line.
func (v *VM) StepOver() error {
	var err error
	if v.HasStopped() {
		return err
	}

	if v.state == BreakState {
		v.state = NoneState
	}

	expSize := v.istack.Len()
	for {
		err = v.StepInto()
		if !(v.state == NoneState && v.istack.Len() > expSize) {
			break
		}
	}

	if v.state == NoneState {
		v.state = BreakState
	}

	return err
}

// HasFailed returns whether VM is in the failed state now. Usually used to
// check status after Run.
func (v *VM) HasFailed() bool {
	return v.state.HasFlag(FaultState)
}

// HasStopped returns whether VM is in Halt or Failed state.
func (v *VM) HasStopped() bool {
	return v.state.HasFlag(HaltState) || v.state.HasFlag(FaultState)
}

// HasHalted returns whether VM is in Halt state.
func (v *VM) HasHalted() bool {
	return v.state.HasFlag(HaltState)
}

// AtBreakpoint returns whether VM is at breakpoint.
func (v *VM) AtBreakpoint() bool {
	return v.state.HasFlag(BreakState)
}

// GetInteropID converts instruction parameter to an interop ID.
func GetInteropID(parameter []byte) uint32 {
	return binary.LittleEndian.Uint32(parameter)
}

// execute performs an instruction cycle in the VM. Acting on the instruction (opcode).
func (v *VM) execute(ctx *Context, op opcode.Opcode, parameter []byte) (err error) {
	// Instead of polluting the whole VM logic with error handling, we will recover
	// each panic at a central point, putting the VM in a fault state and setting error.
	defer func() {
		if errRecover := recover(); errRecover != nil {
			v.state = FaultState
			err = newError(ctx.ip, op, errRecover)
		} else if v.refs > MaxStackSize {
			v.state = FaultState
			err = newError(ctx.ip, op, "stack is too big")
		}
	}()

	if v.getPrice != nil && ctx.ip < len(ctx.prog) {
		v.gasConsumed += v.getPrice(op, parameter)
		if v.GasLimit >= 0 && v.gasConsumed > v.GasLimit {
			panic("gas limit is exceeded")
		}
	}

	if op <= opcode.PUSHINT256 {
		v.estack.PushItem(stackitem.NewBigInteger(bigint.FromBytes(parameter)))
		return
	}

	switch op {
	case opcode.PUSHM1, opcode.PUSH0, opcode.PUSH1, opcode.PUSH2, opcode.PUSH3,
		opcode.PUSH4, opcode.PUSH5, opcode.PUSH6, opcode.PUSH7,
		opcode.PUSH8, opcode.PUSH9, opcode.PUSH10, opcode.PUSH11,
		opcode.PUSH12, opcode.PUSH13, opcode.PUSH14, opcode.PUSH15,
		opcode.PUSH16:
		val := int(op) - int(opcode.PUSH0)
		v.estack.PushItem(stackitem.NewBigInteger(big.NewInt(int64(val))))

	case opcode.PUSHDATA1, opcode.PUSHDATA2, opcode.PUSHDATA4:
		v.estack.PushItem(stackitem.NewByteArray(parameter))

	case opcode.PUSHA:
		n := getJumpOffset(ctx, parameter)
		ptr := stackitem.NewPointerWithHash(n, ctx.prog, ctx.ScriptHash())
		v.estack.PushItem(ptr)

	case opcode.PUSHNULL:
		v.estack.PushItem(stackitem.Null{})

	case opcode.ISNULL:
		_, ok := v.estack.Pop().value.(stackitem.Null)
		v.estack.PushItem(stackitem.Bool(ok))

	case opcode.ISTYPE:
		res := v.estack.Pop().Item()
		v.estack.PushItem(stackitem.Bool(res.Type() == stackitem.Type(parameter[0])))

	case opcode.CONVERT:
		typ := stackitem.Type(parameter[0])
		item := v.estack.Pop().Item()
		result, err := item.Convert(typ)
		if err != nil {
			panic(err)
		}
		v.estack.PushItem(result)

	case opcode.INITSSLOT:
		if parameter[0] == 0 {
			panic("zero argument")
		}
		ctx.static.init(int(parameter[0]))

	case opcode.INITSLOT:
		if ctx.local != nil || ctx.arguments != nil {
			panic("already initialized")
		}
		if parameter[0] == 0 && parameter[1] == 0 {
			panic("zero argument")
		}
		if parameter[0] > 0 {
			ctx.local.init(int(parameter[0]))
		}
		if parameter[1] > 0 {
			sz := int(parameter[1])
			ctx.arguments.init(sz)
			for i := 0; i < sz; i++ {
				ctx.arguments.Set(i, v.estack.Pop().Item(), &v.refs)
			}
		}

	case opcode.LDSFLD0, opcode.LDSFLD1, opcode.LDSFLD2, opcode.LDSFLD3, opcode.LDSFLD4, opcode.LDSFLD5, opcode.LDSFLD6:
		item := ctx.static.Get(int(op - opcode.LDSFLD0))
		v.estack.PushItem(item)

	case opcode.LDSFLD:
		item := ctx.static.Get(int(parameter[0]))
		v.estack.PushItem(item)

	case opcode.STSFLD0, opcode.STSFLD1, opcode.STSFLD2, opcode.STSFLD3, opcode.STSFLD4, opcode.STSFLD5, opcode.STSFLD6:
		item := v.estack.Pop().Item()
		ctx.static.Set(int(op-opcode.STSFLD0), item, &v.refs)

	case opcode.STSFLD:
		item := v.estack.Pop().Item()
		ctx.static.Set(int(parameter[0]), item, &v.refs)

	case opcode.LDLOC0, opcode.LDLOC1, opcode.LDLOC2, opcode.LDLOC3, opcode.LDLOC4, opcode.LDLOC5, opcode.LDLOC6:
		item := ctx.local.Get(int(op - opcode.LDLOC0))
		v.estack.PushItem(item)

	case opcode.LDLOC:
		item := ctx.local.Get(int(parameter[0]))
		v.estack.PushItem(item)

	case opcode.STLOC0, opcode.STLOC1, opcode.STLOC2, opcode.STLOC3, opcode.STLOC4, opcode.STLOC5, opcode.STLOC6:
		item := v.estack.Pop().Item()
		ctx.local.Set(int(op-opcode.STLOC0), item, &v.refs)

	case opcode.STLOC:
		item := v.estack.Pop().Item()
		ctx.local.Set(int(parameter[0]), item, &v.refs)

	case opcode.LDARG0, opcode.LDARG1, opcode.LDARG2, opcode.LDARG3, opcode.LDARG4, opcode.LDARG5, opcode.LDARG6:
		item := ctx.arguments.Get(int(op - opcode.LDARG0))
		v.estack.PushItem(item)

	case opcode.LDARG:
		item := ctx.arguments.Get(int(parameter[0]))
		v.estack.PushItem(item)

	case opcode.STARG0, opcode.STARG1, opcode.STARG2, opcode.STARG3, opcode.STARG4, opcode.STARG5, opcode.STARG6:
		item := v.estack.Pop().Item()
		ctx.arguments.Set(int(op-opcode.STARG0), item, &v.refs)

	case opcode.STARG:
		item := v.estack.Pop().Item()
		ctx.arguments.Set(int(parameter[0]), item, &v.refs)

	case opcode.NEWBUFFER:
		n := toInt(v.estack.Pop().BigInt())
		if n < 0 || n > stackitem.MaxSize {
			panic("invalid size")
		}
		v.estack.PushItem(stackitem.NewBuffer(make([]byte, n)))

	case opcode.MEMCPY:
		n := toInt(v.estack.Pop().BigInt())
		if n < 0 {
			panic("invalid size")
		}
		si := toInt(v.estack.Pop().BigInt())
		if si < 0 {
			panic("invalid source index")
		}
		src := v.estack.Pop().Bytes()
		if sum := si + n; sum < 0 || sum > len(src) {
			panic("size is too big")
		}
		di := toInt(v.estack.Pop().BigInt())
		if di < 0 {
			panic("invalid destination index")
		}
		dst := v.estack.Pop().value.(*stackitem.Buffer).Value().([]byte)
		if sum := di + n; sum < 0 || sum > len(dst) {
			panic("size is too big")
		}
		copy(dst[di:], src[si:si+n])

	case opcode.CAT:
		b := v.estack.Pop().Bytes()
		a := v.estack.Pop().Bytes()
		l := len(a) + len(b)
		if l > stackitem.MaxSize {
			panic(fmt.Sprintf("too big item: %d", l))
		}
		ab := make([]byte, l)
		copy(ab, a)
		copy(ab[len(a):], b)
		v.estack.PushItem(stackitem.NewBuffer(ab))

	case opcode.SUBSTR:
		l := int(v.estack.Pop().BigInt().Int64())
		if l < 0 {
			panic("negative length")
		}
		o := int(v.estack.Pop().BigInt().Int64())
		if o < 0 {
			panic("negative index")
		}
		s := v.estack.Pop().Bytes()
		last := l + o
		if last > len(s) {
			panic("invalid offset")
		}
		res := make([]byte, l)
		copy(res, s[o:last])
		v.estack.PushItem(stackitem.NewBuffer(res))

	case opcode.LEFT:
		l := int(v.estack.Pop().BigInt().Int64())
		if l < 0 {
			panic("negative length")
		}
		s := v.estack.Pop().Bytes()
		if t := len(s); l > t {
			panic("size is too big")
		}
		res := make([]byte, l)
		copy(res, s[:l])
		v.estack.PushItem(stackitem.NewBuffer(res))

	case opcode.RIGHT:
		l := int(v.estack.Pop().BigInt().Int64())
		if l < 0 {
			panic("negative length")
		}
		s := v.estack.Pop().Bytes()
		res := make([]byte, l)
		copy(res, s[len(s)-l:])
		v.estack.PushItem(stackitem.NewBuffer(res))

	case opcode.DEPTH:
		v.estack.PushItem(stackitem.NewBigInteger(big.NewInt(int64(v.estack.Len()))))

	case opcode.DROP:
		if v.estack.Len() < 1 {
			panic("stack is too small")
		}
		v.estack.Pop()

	case opcode.NIP:
		if v.estack.Len() < 2 {
			panic("no second element found")
		}
		_ = v.estack.RemoveAt(1)

	case opcode.XDROP:
		n := int(v.estack.Pop().BigInt().Int64())
		if n < 0 {
			panic("invalid length")
		}
		if v.estack.Len() < n+1 {
			panic("bad index")
		}
		_ = v.estack.RemoveAt(n)

	case opcode.CLEAR:
		v.estack.Clear()

	case opcode.DUP:
		v.estack.Push(v.estack.Dup(0))

	case opcode.OVER:
		if v.estack.Len() < 2 {
			panic("no second element found")
		}
		a := v.estack.Dup(1)
		v.estack.Push(a)

	case opcode.PICK:
		n := int(v.estack.Pop().BigInt().Int64())
		if n < 0 {
			panic("negative stack item returned")
		}
		if v.estack.Len() < n+1 {
			panic("no nth element found")
		}
		a := v.estack.Dup(n)
		v.estack.Push(a)

	case opcode.TUCK:
		if v.estack.Len() < 2 {
			panic("too short stack to TUCK")
		}
		a := v.estack.Dup(0)
		v.estack.InsertAt(a, 2)

	case opcode.SWAP:
		err := v.estack.Swap(1, 0)
		if err != nil {
			panic(err.Error())
		}

	case opcode.ROT:
		err := v.estack.Roll(2)
		if err != nil {
			panic(err.Error())
		}

	case opcode.ROLL:
		n := int(v.estack.Pop().BigInt().Int64())
		err := v.estack.Roll(n)
		if err != nil {
			panic(err.Error())
		}

	case opcode.REVERSE3, opcode.REVERSE4, opcode.REVERSEN:
		n := 3
		switch op {
		case opcode.REVERSE4:
			n = 4
		case opcode.REVERSEN:
			n = int(v.estack.Pop().BigInt().Int64())
		}
		if err := v.estack.ReverseTop(n); err != nil {
			panic(err.Error())
		}

	// Bit operations.
	case opcode.INVERT:
		i := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.NewBigInteger(new(big.Int).Not(i)))

	case opcode.AND:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.NewBigInteger(new(big.Int).And(b, a)))

	case opcode.OR:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.NewBigInteger(new(big.Int).Or(b, a)))

	case opcode.XOR:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.NewBigInteger(new(big.Int).Xor(b, a)))

	case opcode.EQUAL, opcode.NOTEQUAL:
		if v.estack.Len() < 2 {
			panic("need a pair of elements on the stack")
		}
		b := v.estack.Pop()
		a := v.estack.Pop()
		res := stackitem.Bool(a.value.Equals(b.value) == (op == opcode.EQUAL))
		v.estack.PushItem(res)

	// Numeric operations.
	case opcode.SIGN:
		x := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.NewBigInteger(big.NewInt(int64(x.Sign()))))

	case opcode.ABS:
		x := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.NewBigInteger(new(big.Int).Abs(x)))

	case opcode.NEGATE:
		x := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.NewBigInteger(new(big.Int).Neg(x)))

	case opcode.INC:
		x := v.estack.Pop().BigInt()
		a := new(big.Int).Add(x, bigOne)
		v.estack.PushItem(stackitem.NewBigInteger(a))

	case opcode.DEC:
		x := v.estack.Pop().BigInt()
		a := new(big.Int).Sub(x, bigOne)
		v.estack.PushItem(stackitem.NewBigInteger(a))

	case opcode.ADD:
		a := v.estack.Pop().BigInt()
		b := v.estack.Pop().BigInt()

		c := new(big.Int).Add(a, b)
		v.estack.PushItem(stackitem.NewBigInteger(c))

	case opcode.SUB:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()

		c := new(big.Int).Sub(a, b)
		v.estack.PushItem(stackitem.NewBigInteger(c))

	case opcode.MUL:
		a := v.estack.Pop().BigInt()
		b := v.estack.Pop().BigInt()

		c := new(big.Int).Mul(a, b)
		v.estack.PushItem(stackitem.NewBigInteger(c))

	case opcode.DIV:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()

		v.estack.PushItem(stackitem.NewBigInteger(new(big.Int).Quo(a, b)))

	case opcode.MOD:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()

		v.estack.PushItem(stackitem.NewBigInteger(new(big.Int).Rem(a, b)))

	case opcode.POW:
		exp := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		if ei := exp.Uint64(); !exp.IsUint64() || ei > maxSHLArg {
			panic("invalid exponent")
		}
		v.estack.PushItem(stackitem.NewBigInteger(new(big.Int).Exp(a, exp, nil)))

	case opcode.SQRT:
		a := v.estack.Pop().BigInt()
		if a.Sign() == -1 {
			panic("negative value")
		}

		v.estack.PushItem(stackitem.NewBigInteger(new(big.Int).Sqrt(a)))

	case opcode.SHL, opcode.SHR:
		b := v.estack.Pop().BigInt().Int64()
		if b == 0 {
			return
		} else if b < 0 || b > maxSHLArg {
			panic(fmt.Sprintf("operand must be between %d and %d", 0, maxSHLArg))
		}
		a := v.estack.Pop().BigInt()

		var item big.Int
		if op == opcode.SHL {
			item.Lsh(a, uint(b))
		} else {
			item.Rsh(a, uint(b))
		}

		v.estack.PushItem(stackitem.NewBigInteger(&item))

	case opcode.NOT:
		x := v.estack.Pop().Bool()
		v.estack.PushItem(stackitem.Bool(!x))

	case opcode.BOOLAND:
		b := v.estack.Pop().Bool()
		a := v.estack.Pop().Bool()
		v.estack.PushItem(stackitem.Bool(a && b))

	case opcode.BOOLOR:
		b := v.estack.Pop().Bool()
		a := v.estack.Pop().Bool()
		v.estack.PushItem(stackitem.Bool(a || b))

	case opcode.NZ:
		x := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.Bool(x.Sign() != 0))

	case opcode.NUMEQUAL:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.Bool(a.Cmp(b) == 0))

	case opcode.NUMNOTEQUAL:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.Bool(a.Cmp(b) != 0))

	case opcode.LT, opcode.LE, opcode.GT, opcode.GE:
		eb := v.estack.Pop()
		ea := v.estack.Pop()
		_, aNil := ea.Item().(stackitem.Null)
		_, bNil := eb.Item().(stackitem.Null)

		res := !aNil && !bNil
		if res {
			cmp := ea.BigInt().Cmp(eb.BigInt())
			switch op {
			case opcode.LT:
				res = cmp == -1
			case opcode.LE:
				res = cmp <= 0
			case opcode.GT:
				res = cmp == 1
			case opcode.GE:
				res = cmp >= 0
			}
		}
		v.estack.PushItem(stackitem.Bool(res))

	case opcode.MIN:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		val := a
		if a.Cmp(b) == 1 {
			val = b
		}
		v.estack.PushItem(stackitem.NewBigInteger(val))

	case opcode.MAX:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		val := a
		if a.Cmp(b) == -1 {
			val = b
		}
		v.estack.PushItem(stackitem.NewBigInteger(val))

	case opcode.WITHIN:
		b := v.estack.Pop().BigInt()
		a := v.estack.Pop().BigInt()
		x := v.estack.Pop().BigInt()
		v.estack.PushItem(stackitem.Bool(a.Cmp(x) <= 0 && x.Cmp(b) == -1))

	// Object operations
	case opcode.NEWARRAY0:
		v.estack.PushItem(stackitem.NewArray([]stackitem.Item{}))

	case opcode.NEWARRAY, opcode.NEWARRAYT, opcode.NEWSTRUCT:
		n := toInt(v.estack.Pop().BigInt())
		if n < 0 || n > MaxStackSize {
			panic("wrong number of elements")
		}
		typ := stackitem.AnyT
		if op == opcode.NEWARRAYT {
			typ = stackitem.Type(parameter[0])
		}
		items := makeArrayOfType(int(n), typ)
		var res stackitem.Item
		if op == opcode.NEWSTRUCT {
			res = stackitem.NewStruct(items)
		} else {
			res = stackitem.NewArray(items)
		}
		v.estack.PushItem(res)

	case opcode.NEWSTRUCT0:
		v.estack.PushItem(stackitem.NewStruct([]stackitem.Item{}))

	case opcode.APPEND:
		itemElem := v.estack.Pop()
		arrElem := v.estack.Pop()

		val := cloneIfStruct(itemElem.value)

		switch t := arrElem.value.(type) {
		case *stackitem.Array:
			t.Append(val)
		case *stackitem.Struct:
			t.Append(val)
		default:
			panic("APPEND: not of underlying type Array")
		}

		v.refs.Add(val)

	case opcode.PACKMAP:
		n := toInt(v.estack.Pop().BigInt())
		if n < 0 || n*2 > v.estack.Len() {
			panic("invalid length")
		}

		items := make([]stackitem.MapElement, n)
		for i := 0; i < n; i++ {
			key := v.estack.Pop()
			validateMapKey(key)
			val := v.estack.Pop().value
			items[i].Key = key.value
			items[i].Value = val
		}
		v.estack.PushItem(stackitem.NewMapWithValue(items))

	case opcode.PACKSTRUCT, opcode.PACK:
		n := toInt(v.estack.Pop().BigInt())
		if n < 0 || n > v.estack.Len() {
			panic("OPACK: invalid length")
		}

		items := make([]stackitem.Item, n)
		for i := 0; i < n; i++ {
			items[i] = v.estack.Pop().value
		}

		var res stackitem.Item
		if op == opcode.PACK {
			res = stackitem.NewArray(items)
		} else {
			res = stackitem.NewStruct(items)
		}
		v.estack.PushItem(res)

	case opcode.UNPACK:
		e := v.estack.Pop()
		var arr []stackitem.Item
		var l int

		switch t := e.value.(type) {
		case *stackitem.Array:
			arr = t.Value().([]stackitem.Item)
		case *stackitem.Struct:
			arr = t.Value().([]stackitem.Item)
		case *stackitem.Map:
			m := t.Value().([]stackitem.MapElement)
			l = len(m)
			for i := l - 1; i >= 0; i-- {
				v.estack.PushItem(m[i].Value)
				v.estack.PushItem(m[i].Key)
			}
		default:
			panic("element is not an array/struct/map")
		}
		if arr != nil {
			l = len(arr)
			for i := l - 1; i >= 0; i-- {
				v.estack.PushItem(arr[i])
			}
		}
		v.estack.PushItem(stackitem.NewBigInteger(big.NewInt(int64(l))))

	case opcode.PICKITEM:
		key := v.estack.Pop()
		validateMapKey(key)

		obj := v.estack.Pop()

		switch t := obj.value.(type) {
		// Struct and Array items have their underlying value as []Item.
		case *stackitem.Array, *stackitem.Struct:
			index := toInt(key.BigInt())
			arr := t.Value().([]stackitem.Item)
			if index < 0 || index >= len(arr) {
				msg := fmt.Sprintf("The value %d is out of range.", index)
				v.throw(stackitem.NewByteArray([]byte(msg)))
				return
			}
			item := arr[index].Dup()
			v.estack.PushItem(item)
		case *stackitem.Map:
			index := t.Index(key.Item())
			if index < 0 {
				v.throw(stackitem.NewByteArray([]byte("Key not found in Map")))
				return
			}
			v.estack.PushItem(t.Value().([]stackitem.MapElement)[index].Value.Dup())
		default:
			index := toInt(key.BigInt())
			arr := obj.Bytes()
			if index < 0 || index >= len(arr) {
				msg := fmt.Sprintf("The value %d is out of range.", index)
				v.throw(stackitem.NewByteArray([]byte(msg)))
				return
			}
			item := arr[index]
			v.estack.PushItem(stackitem.NewBigInteger(big.NewInt(int64(item))))
		}

	case opcode.SETITEM:
		item := v.estack.Pop().value
		key := v.estack.Pop()
		validateMapKey(key)

		obj := v.estack.Pop()

		switch t := obj.value.(type) {
		// Struct and Array items have their underlying value as []Item.
		case *stackitem.Array, *stackitem.Struct:
			arr := t.Value().([]stackitem.Item)
			index := toInt(key.BigInt())
			if index < 0 || index >= len(arr) {
				msg := fmt.Sprintf("The value %d is out of range.", index)
				v.throw(stackitem.NewByteArray([]byte(msg)))
				return
			}
			v.refs.Remove(arr[index])
			arr[index] = item
			v.refs.Add(arr[index])
		case *stackitem.Map:
			if i := t.Index(key.value); i >= 0 {
				v.refs.Remove(t.Value().([]stackitem.MapElement)[i].Value)
			}
			t.Add(key.value, item)
			v.refs.Add(item)

		case *stackitem.Buffer:
			index := toInt(key.BigInt())
			if index < 0 || index >= t.Len() {
				msg := fmt.Sprintf("The value %d is out of range.", index)
				v.throw(stackitem.NewByteArray([]byte(msg)))
				return
			}
			bi, err := item.TryInteger()
			b := toInt(bi)
			if err != nil || b < math.MinInt8 || b > math.MaxUint8 {
				panic("invalid value")
			}
			t.Value().([]byte)[index] = byte(b)

		default:
			panic(fmt.Sprintf("SETITEM: invalid item type %s", t))
		}

	case opcode.REVERSEITEMS:
		item := v.estack.Pop()
		switch t := item.value.(type) {
		case *stackitem.Array, *stackitem.Struct:
			a := t.Value().([]stackitem.Item)
			for i, j := 0, len(a)-1; i < j; i, j = i+1, j-1 {
				a[i], a[j] = a[j], a[i]
			}
		case *stackitem.Buffer:
			b := t.Value().([]byte)
			slice.Reverse(b)
		default:
			panic(fmt.Sprintf("invalid item type %s", t))
		}
	case opcode.REMOVE:
		key := v.estack.Pop()
		validateMapKey(key)

		elem := v.estack.Pop()
		switch t := elem.value.(type) {
		case *stackitem.Array:
			a := t.Value().([]stackitem.Item)
			k := int(key.BigInt().Int64())
			if k < 0 || k >= len(a) {
				panic("REMOVE: invalid index")
			}
			v.refs.Remove(a[k])
			t.Remove(k)
		case *stackitem.Struct:
			a := t.Value().([]stackitem.Item)
			k := int(key.BigInt().Int64())
			if k < 0 || k >= len(a) {
				panic("REMOVE: invalid index")
			}
			v.refs.Remove(a[k])
			t.Remove(k)
		case *stackitem.Map:
			index := t.Index(key.Item())
			// NEO 2.0 doesn't error on missing key.
			if index >= 0 {
				v.refs.Remove(t.Value().([]stackitem.MapElement)[index].Value)
				t.Drop(index)
			}
		default:
			panic("REMOVE: invalid type")
		}

	case opcode.CLEARITEMS:
		elem := v.estack.Pop()
		switch t := elem.value.(type) {
		case *stackitem.Array:
			for _, item := range t.Value().([]stackitem.Item) {
				v.refs.Remove(item)
			}
			t.Clear()
		case *stackitem.Struct:
			for _, item := range t.Value().([]stackitem.Item) {
				v.refs.Remove(item)
			}
			t.Clear()
		case *stackitem.Map:
			for i := range t.Value().([]stackitem.MapElement) {
				v.refs.Remove(t.Value().([]stackitem.MapElement)[i].Value)
			}
			t.Clear()
		default:
			panic("CLEARITEMS: invalid type")
		}

	case opcode.POPITEM:
		arr := v.estack.Pop().Item()
		elems := arr.Value().([]stackitem.Item)
		index := len(elems) - 1
		elem := elems[index]
		switch item := arr.(type) {
		case *stackitem.Array:
			item.Remove(index)
		case *stackitem.Struct:
			item.Remove(index)
		}
		v.refs.Remove(elem)
		v.estack.PushItem(elem)

	case opcode.SIZE:
		elem := v.estack.Pop()
		var res int
		// Cause there is no native (byte) item type here, hence we need to check
		// the type of the item for array size operations.
		switch t := elem.Value().(type) {
		case []stackitem.Item:
			res = len(t)
		case []stackitem.MapElement:
			res = len(t)
		default:
			res = len(elem.Bytes())
		}
		v.estack.PushItem(stackitem.NewBigInteger(big.NewInt(int64(res))))

	case opcode.JMP, opcode.JMPL, opcode.JMPIF, opcode.JMPIFL, opcode.JMPIFNOT, opcode.JMPIFNOTL,
		opcode.JMPEQ, opcode.JMPEQL, opcode.JMPNE, opcode.JMPNEL,
		opcode.JMPGT, opcode.JMPGTL, opcode.JMPGE, opcode.JMPGEL,
		opcode.JMPLT, opcode.JMPLTL, opcode.JMPLE, opcode.JMPLEL:
		offset := getJumpOffset(ctx, parameter)
		cond := true
		switch op {
		case opcode.JMP, opcode.JMPL:
		case opcode.JMPIF, opcode.JMPIFL, opcode.JMPIFNOT, opcode.JMPIFNOTL:
			cond = v.estack.Pop().Bool() == (op == opcode.JMPIF || op == opcode.JMPIFL)
		default:
			b := v.estack.Pop().BigInt()
			a := v.estack.Pop().BigInt()
			cond = getJumpCondition(op, a, b)
		}

		if cond {
			ctx.Jump(offset)
		}

	case opcode.CALL, opcode.CALLL:
		// Note: jump offset must be calculated regarding to new context,
		// but it is cloned and thus has the same script and instruction pointer.
		v.call(ctx, getJumpOffset(ctx, parameter))

	case opcode.CALLA:
		ptr := v.estack.Pop().Item().(*stackitem.Pointer)
		if ptr.ScriptHash() != ctx.ScriptHash() {
			panic("invalid script in pointer")
		}

		v.call(ctx, ptr.Position())

	case opcode.CALLT:
		id := int32(binary.LittleEndian.Uint16(parameter))
		if err := v.LoadToken(id); err != nil {
			panic(err)
		}

	case opcode.SYSCALL:
		interopID := GetInteropID(parameter)
		err := v.SyscallHandler(v, interopID)
		if err != nil {
			panic(fmt.Sprintf("failed to invoke syscall %d: %s", interopID, err))
		}

	case opcode.RET:
		oldCtx := v.istack.Pop().value.(*Context)
		oldEstack := v.estack

		v.unloadContext(oldCtx)
		if v.istack.Len() == 0 {
			v.state = HaltState
			break
		}

		newEstack := v.Context().estack
		if oldEstack != newEstack {
			if oldCtx.retCount >= 0 && oldEstack.Len() != oldCtx.retCount {
				panic(fmt.Errorf("invalid return values count: expected %d, got %d",
					oldCtx.retCount, oldEstack.Len()))
			}
			rvcount := oldEstack.Len()
			for i := rvcount; i > 0; i-- {
				elem := oldEstack.RemoveAt(i - 1)
				newEstack.Push(elem)
			}
			v.estack = newEstack
		}

	case opcode.NEWMAP:
		v.estack.PushItem(stackitem.NewMap())

	case opcode.KEYS:
		if v.estack.Len() == 0 {
			panic("no argument")
		}
		item := v.estack.Pop()

		m, ok := item.value.(*stackitem.Map)
		if !ok {
			panic("not a Map")
		}

		arr := make([]stackitem.Item, 0, m.Len())
		for k := range m.Value().([]stackitem.MapElement) {
			arr = append(arr, m.Value().([]stackitem.MapElement)[k].Key.Dup())
		}
		v.estack.PushItem(stackitem.NewArray(arr))

	case opcode.VALUES:
		if v.estack.Len() == 0 {
			panic("no argument")
		}
		item := v.estack.Pop()

		var arr []stackitem.Item
		switch t := item.value.(type) {
		case *stackitem.Array, *stackitem.Struct:
			src := t.Value().([]stackitem.Item)
			arr = make([]stackitem.Item, len(src))
			for i := range src {
				arr[i] = cloneIfStruct(src[i])
			}
		case *stackitem.Map:
			arr = make([]stackitem.Item, 0, t.Len())
			for k := range t.Value().([]stackitem.MapElement) {
				arr = append(arr, cloneIfStruct(t.Value().([]stackitem.MapElement)[k].Value))
			}
		default:
			panic("not a Map, Array or Struct")
		}

		v.estack.PushItem(stackitem.NewArray(arr))

	case opcode.HASKEY:
		if v.estack.Len() < 2 {
			panic("not enough arguments")
		}
		key := v.estack.Pop()
		validateMapKey(key)

		c := v.estack.Pop()
		var res bool
		switch t := c.value.(type) {
		case *stackitem.Array, *stackitem.Struct:
			index := key.BigInt().Int64()
			if index < 0 {
				panic("negative index")
			}
			res = index < int64(len(c.Array()))
		case *stackitem.Map:
			res = t.Has(key.Item())
		case *stackitem.Buffer:
			index := key.BigInt().Int64()
			if index < 0 {
				panic("negative index")
			}
			res = index < int64(t.Len())
		default:
			panic("wrong collection type")
		}
		v.estack.PushItem(stackitem.Bool(res))

	case opcode.NOP:
		// unlucky ^^

	case opcode.THROW:
		v.throw(v.estack.Pop().Item())

	case opcode.ABORT:
		panic("ABORT")

	case opcode.ASSERT:
		if !v.estack.Pop().Bool() {
			panic("ASSERT failed")
		}

	case opcode.TRY, opcode.TRYL:
		catchP, finallyP := getTryParams(op, parameter)
		if ctx.tryStack.Len() >= MaxTryNestingDepth {
			panic("maximum TRY depth exceeded")
		}
		cOffset := getJumpOffset(ctx, catchP)
		fOffset := getJumpOffset(ctx, finallyP)
		if cOffset == ctx.ip && fOffset == ctx.ip {
			panic("invalid offset for TRY*")
		} else if cOffset == ctx.ip {
			cOffset = -1
		} else if fOffset == ctx.ip {
			fOffset = -1
		}
		eCtx := newExceptionHandlingContext(cOffset, fOffset)
		ctx.tryStack.PushItem(eCtx)

	case opcode.ENDTRY, opcode.ENDTRYL:
		eCtx := ctx.tryStack.Peek(0).Value().(*exceptionHandlingContext)
		if eCtx.State == eFinally {
			panic("invalid exception handling state during ENDTRY*")
		}
		eOffset := getJumpOffset(ctx, parameter)
		if eCtx.HasFinally() {
			eCtx.State = eFinally
			eCtx.EndOffset = eOffset
			eOffset = eCtx.FinallyOffset
		} else {
			ctx.tryStack.Pop()
		}
		ctx.Jump(eOffset)

	case opcode.ENDFINALLY:
		if v.uncaughtException != nil {
			v.handleException()
			return
		}
		eCtx := ctx.tryStack.Pop().Value().(*exceptionHandlingContext)
		ctx.Jump(eCtx.EndOffset)

	default:
		panic(fmt.Sprintf("unknown opcode %s", op.String()))
	}
	return
}

func (v *VM) unloadContext(ctx *Context) {
	if ctx.local != nil {
		ctx.local.Clear(&v.refs)
	}
	if ctx.arguments != nil {
		ctx.arguments.Clear(&v.refs)
	}
	currCtx := v.Context()
	if ctx.static != nil && currCtx != nil && ctx.static != currCtx.static {
		ctx.static.Clear(&v.refs)
	}
}

// getTryParams splits TRY(L) instruction parameter into offsets for catch and finally blocks.
func getTryParams(op opcode.Opcode, p []byte) ([]byte, []byte) {
	i := 1
	if op == opcode.TRYL {
		i = 4
	}
	return p[:i], p[i:]
}

// getJumpCondition performs opcode specific comparison of a and b.
func getJumpCondition(op opcode.Opcode, a, b *big.Int) bool {
	cmp := a.Cmp(b)
	switch op {
	case opcode.JMPEQ, opcode.JMPEQL:
		return cmp == 0
	case opcode.JMPNE, opcode.JMPNEL:
		return cmp != 0
	case opcode.JMPGT, opcode.JMPGTL:
		return cmp > 0
	case opcode.JMPGE, opcode.JMPGEL:
		return cmp >= 0
	case opcode.JMPLT, opcode.JMPLTL:
		return cmp < 0
	case opcode.JMPLE, opcode.JMPLEL:
		return cmp <= 0
	default:
		panic(fmt.Sprintf("invalid JMP* opcode: %s", op))
	}
}

func (v *VM) throw(item stackitem.Item) {
	v.uncaughtException = item
	v.handleException()
}

// Call calls method by offset using new execution context.
func (v *VM) Call(offset int) {
	v.call(v.Context(), offset)
}

// call is an internal representation of Call, which does not
// affect the invocation counter and is only being used by vm
// package.
func (v *VM) call(ctx *Context, offset int) {
	v.checkInvocationStackSize()
	newCtx := ctx.Copy()
	newCtx.retCount = -1
	newCtx.local = nil
	newCtx.arguments = nil
	initStack(&newCtx.tryStack, "exception", nil)
	newCtx.NEF = ctx.NEF
	v.istack.PushItem(newCtx)
	newCtx.Jump(offset)
}

// getJumpOffset returns instruction number in a current context
// to a which JMP should be performed.
// parameter should have length either 1 or 4 and
// is interpreted as little-endian.
func getJumpOffset(ctx *Context, parameter []byte) int {
	offset, _, err := calcJumpOffset(ctx, parameter)
	if err != nil {
		panic(err)
	}
	return offset
}

// calcJumpOffset returns absolute and relative offset of JMP/CALL/TRY instructions
// either in short (1-byte) or long (4-byte) form.
func calcJumpOffset(ctx *Context, parameter []byte) (int, int, error) {
	var rOffset int32
	switch l := len(parameter); l {
	case 1:
		rOffset = int32(int8(parameter[0]))
	case 4:
		rOffset = int32(binary.LittleEndian.Uint32(parameter))
	default:
		_, curr := ctx.CurrInstr()
		return 0, 0, fmt.Errorf("invalid %s parameter length: %d", curr, l)
	}
	offset := ctx.ip + int(rOffset)
	if offset < 0 || offset > len(ctx.prog) {
		return 0, 0, fmt.Errorf("invalid offset %d ip at %d", offset, ctx.ip)
	}

	return offset, int(rOffset), nil
}

func (v *VM) handleException() {
	for pop := 0; pop < v.istack.Len(); pop++ {
		ictxv := v.istack.Peek(pop)
		ictx := ictxv.value.(*Context)
		for j := 0; j < ictx.tryStack.Len(); j++ {
			e := ictx.tryStack.Peek(j)
			ectx := e.Value().(*exceptionHandlingContext)
			if ectx.State == eFinally || (ectx.State == eCatch && !ectx.HasFinally()) {
				ictx.tryStack.Pop()
				j = -1
				continue
			}
			for i := 0; i < pop; i++ {
				ctx := v.istack.Pop().value.(*Context)
				v.unloadContext(ctx)
			}
			if ectx.State == eTry && ectx.HasCatch() {
				ectx.State = eCatch
				v.estack.PushItem(v.uncaughtException)
				v.uncaughtException = nil
				ictx.Jump(ectx.CatchOffset)
			} else {
				ectx.State = eFinally
				ictx.Jump(ectx.FinallyOffset)
			}
			return
		}
	}
	throwUnhandledException(v.uncaughtException)
}

// throwUnhandledException gets exception message from the provided stackitem and panics.
func throwUnhandledException(item stackitem.Item) {
	msg := "unhandled exception"
	switch item.Type() {
	case stackitem.ArrayT:
		if arr := item.Value().([]stackitem.Item); len(arr) > 0 {
			data, err := arr[0].TryBytes()
			if err == nil {
				msg = fmt.Sprintf("%s: %q", msg, string(data))
			}
		}
	default:
		data, err := item.TryBytes()
		if err == nil {
			msg = fmt.Sprintf("%s: %q", msg, string(data))
		}
	}
	panic(msg)
}

// CheckMultisigPar checks if sigs contains sufficient valid signatures.
func CheckMultisigPar(v *VM, curve elliptic.Curve, h []byte, pkeys [][]byte, sigs [][]byte) bool {
	if len(sigs) == 1 {
		return checkMultisig1(v, curve, h, pkeys, sigs[0])
	}

	k1, k2 := 0, len(pkeys)-1
	s1, s2 := 0, len(sigs)-1

	type task struct {
		pub    *keys.PublicKey
		signum int
	}

	type verify struct {
		ok     bool
		signum int
	}

	worker := func(ch <-chan task, result chan verify) {
		for {
			t, ok := <-ch
			if !ok {
				return
			}

			result <- verify{
				signum: t.signum,
				ok:     t.pub.Verify(sigs[t.signum], h),
			}
		}
	}

	const workerCount = 3
	tasks := make(chan task, 2)
	results := make(chan verify, len(sigs))
	for i := 0; i < workerCount; i++ {
		go worker(tasks, results)
	}

	tasks <- task{pub: bytesToPublicKey(pkeys[k1], curve), signum: s1}
	tasks <- task{pub: bytesToPublicKey(pkeys[k2], curve), signum: s2}

	sigok := true
	taskCount := 2

loop:
	for r := range results {
		goingForward := true

		taskCount--
		if r.signum == s2 {
			goingForward = false
		}
		if k1+1 == k2 {
			sigok = r.ok && s1+1 == s2
			if taskCount != 0 && sigok {
				continue
			}
			break loop
		} else if r.ok {
			if s1+1 == s2 {
				if taskCount != 0 && sigok {
					continue
				}
				break loop
			}
			if goingForward {
				s1++
			} else {
				s2--
			}
		}

		var nextSig, nextKey int
		if goingForward {
			k1++
			nextSig = s1
			nextKey = k1
		} else {
			k2--
			nextSig = s2
			nextKey = k2
		}
		taskCount++
		tasks <- task{pub: bytesToPublicKey(pkeys[nextKey], curve), signum: nextSig}
	}

	close(tasks)

	return sigok
}

func checkMultisig1(v *VM, curve elliptic.Curve, h []byte, pkeys [][]byte, sig []byte) bool {
	for i := range pkeys {
		pkey := bytesToPublicKey(pkeys[i], curve)
		if pkey.Verify(sig, h) {
			return true
		}
	}

	return false
}

func cloneIfStruct(item stackitem.Item) stackitem.Item {
	switch it := item.(type) {
	case *stackitem.Struct:
		ret, err := it.Clone()
		if err != nil {
			panic(err)
		}
		return ret
	default:
		return it
	}
}

func makeArrayOfType(n int, typ stackitem.Type) []stackitem.Item {
	if !typ.IsValid() {
		panic(fmt.Sprintf("invalid stack item type: %d", typ))
	}
	items := make([]stackitem.Item, n)
	for i := range items {
		switch typ {
		case stackitem.BooleanT:
			items[i] = stackitem.NewBool(false)
		case stackitem.IntegerT:
			items[i] = stackitem.NewBigInteger(big.NewInt(0))
		case stackitem.ByteArrayT:
			items[i] = stackitem.NewByteArray([]byte{})
		default:
			items[i] = stackitem.Null{}
		}
	}
	return items
}

func validateMapKey(key Element) {
	item := key.Item()
	if item == nil {
		panic("no key found")
	}
	if err := stackitem.IsValidMapKey(item); err != nil {
		panic(err)
	}
}

func (v *VM) checkInvocationStackSize() {
	if v.istack.Len() >= MaxInvocationStackSize {
		panic("invocation stack is too big")
	}
}

// bytesToPublicKey is a helper deserializing keys using cache and panicing on
// error.
func bytesToPublicKey(b []byte, curve elliptic.Curve) *keys.PublicKey {
	pkey, err := keys.NewPublicKeyFromBytes(b, curve)
	if err != nil {
		panic(err.Error())
	}
	return pkey
}

// GetCallingScriptHash implements ScriptHashGetter interface.
func (v *VM) GetCallingScriptHash() util.Uint160 {
	return v.Context().callingScriptHash
}

// GetEntryScriptHash implements ScriptHashGetter interface.
func (v *VM) GetEntryScriptHash() util.Uint160 {
	return v.getContextScriptHash(v.istack.Len() - 1)
}

// GetCurrentScriptHash implements ScriptHashGetter interface.
func (v *VM) GetCurrentScriptHash() util.Uint160 {
	return v.getContextScriptHash(0)
}

// toInt converts an item to a 32-bit int.
func toInt(i *big.Int) int {
	if !i.IsInt64() {
		panic("not an int32")
	}
	n := i.Int64()
	if n < math.MinInt32 || n > math.MaxInt32 {
		panic("not an int32")
	}
	return int(n)
}

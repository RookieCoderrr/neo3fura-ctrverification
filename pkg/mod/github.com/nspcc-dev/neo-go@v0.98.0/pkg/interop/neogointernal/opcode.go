package neogointernal

// Opcode0NoReturn emits opcode without arguments.
func Opcode0NoReturn(op string) {
}

// Opcode1 emits opcode with 1 argument.
func Opcode1(op string, arg interface{}) interface{} {
	return nil
}

// Opcode2 emits opcode with 2 arguments.
func Opcode2(op string, arg1, arg2 interface{}) interface{} {
	return nil
}

// Opcode2NoReturn emits opcode with 2 arguments.
func Opcode2NoReturn(op string, arg1, arg2 interface{}) {
}

// Opcode3 emits opcode with 3 arguments.
func Opcode3(op string, arg1, arg2, arg3 interface{}) interface{} {
	return nil
}

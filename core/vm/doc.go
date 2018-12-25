
/*package vm implements the Aurora Virtual Machine.

The vm package implements two EVMs, a byte code VM and a JIT VM. The BC
(Byte Code) VM loops over a set of bytes and executes them according to the set
of rules defined in the Aurora yellow paper. When the BC VM is invoked it
invokes the JIT VM in a separate goroutine and compiles the byte code in JIT
instructions.

The JIT VM, when invoked, loops around a set of pre-defined instructions until
it either runs of gas, causes an internal error, returns or stops.

The JIT optimiser attempts to pre-compile instructions in to chunks or segments
such as multiple PUSH operations and static JUMPs. It does this by analysing the
opcodes and attempts to match certain regions to known sets. Whenever the
optimiser finds said segments it creates a new instruction and replaces the
first occurrence in the sequence.
*/package vm

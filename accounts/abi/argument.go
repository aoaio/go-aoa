package abi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type Argument struct {
	Name    string
	Type    Type
	Indexed bool
}

type Arguments []Argument

func (argument *Argument) UnmarshalJSON(data []byte) error {
	var extarg struct {
		Name    string
		Type    string
		Indexed bool
	}
	err := json.Unmarshal(data, &extarg)
	if err != nil {
		return fmt.Errorf("argument json err: %v", err)
	}

	argument.Type, err = NewType(extarg.Type)
	if err != nil {
		return err
	}
	argument.Name = extarg.Name
	argument.Indexed = extarg.Indexed

	return nil
}

func (arguments Arguments) LengthNonIndexed() int {
	out := 0
	for _, arg := range arguments {
		if !arg.Indexed {
			out++
		}
	}
	return out
}

func (arguments Arguments) isTuple() bool {
	return len(arguments) > 1
}

func (arguments Arguments) Unpack(v interface{}, data []byte) error {
	if arguments.isTuple() {
		return arguments.unpackTuple(v, data)
	}
	return arguments.unpackAtomic(v, data)
}

func (arguments Arguments) unpackTuple(v interface{}, output []byte) error {
	valueOf := reflect.ValueOf(v)
	if reflect.Ptr != valueOf.Kind() {
		return fmt.Errorf("abi: Unpack(non-pointer %T)", v)
	}

	var (
		value = valueOf.Elem()
		typ   = value.Type()
		kind  = value.Kind()
	)

	if err := requireUnpackKind(value, typ, kind, arguments); err != nil {
		return err
	}
	if kind == reflect.Struct {
		exists := make(map[string]bool)
		for _, arg := range arguments {
			field := capitalise(arg.Name)
			if field == "" {
				return fmt.Errorf("abi: purely underscored output cannot unpack to struct")
			}
			if exists[field] {
				return fmt.Errorf("abi: multiple outputs mapping to the same struct field '%s'", field)
			}
			exists[field] = true
		}
	}

	i, j := -1, 0
	for _, arg := range arguments {

		if arg.Indexed {
			continue
		}
		i++
		marshalledValue, err := toGoType((i+j)*32, arg.Type, output)
		if err != nil {
			return err
		}

		if arg.Type.T == ArrayTy {
			j += arg.Type.Size - 1
		}

		reflectValue := reflect.ValueOf(marshalledValue)

		switch kind {
		case reflect.Struct:
			name := capitalise(arg.Name)
			for j := 0; j < typ.NumField(); j++ {
				if typ.Field(j).Name == name {
					if err := set(value.Field(j), reflectValue, arg); err != nil {
						return err
					}
				}
			}
		case reflect.Slice, reflect.Array:
			if value.Len() < i {
				return fmt.Errorf("abi: insufficient number of arguments for unpack, want %d, got %d", len(arguments), value.Len())
			}
			v := value.Index(i)
			if err := requireAssignable(v, reflectValue); err != nil {
				return err
			}

			if err := set(v.Elem(), reflectValue, arg); err != nil {
				return err
			}
		default:
			return fmt.Errorf("abi:[2] cannot unmarshal tuple in to %v", typ)
		}
	}
	return nil
}

func (arguments Arguments) unpackAtomic(v interface{}, output []byte) error {
	valueOf := reflect.ValueOf(v)
	if reflect.Ptr != valueOf.Kind() {
		return fmt.Errorf("abi: Unpack(non-pointer %T)", v)
	}
	arg := arguments[0]
	if arg.Indexed {
		return fmt.Errorf("abi: attempting to unpack indexed variable into element.")
	}

	value := valueOf.Elem()

	marshalledValue, err := toGoType(0, arg.Type, output)
	if err != nil {
		return err
	}
	return set(value, reflect.ValueOf(marshalledValue), arg)
}

func (arguments Arguments) Pack(args ...interface{}) ([]byte, error) {
	abiArgs := arguments
	if len(args) != len(abiArgs) {
		return nil, fmt.Errorf("argument count mismatch: %d for %d", len(args), len(abiArgs))
	}

	var variableInput []byte

	inputOffset := 0
	for _, abiArg := range abiArgs {
		if abiArg.Type.T == ArrayTy {
			inputOffset += 32 * abiArg.Type.Size
		} else {
			inputOffset += 32
		}
	}

	var ret []byte
	for i, a := range args {
		input := abiArgs[i]
		packed, err := input.Type.pack(reflect.ValueOf(a))
		if err != nil {
			return nil, err
		}

		if input.Type.requiresLengthPrefix() {
			offset := inputOffset + len(variableInput)
			ret = append(ret, packNum(reflect.ValueOf(offset))...)
			variableInput = append(variableInput, packed...)
		} else {
			ret = append(ret, packed...)
		}
	}
	ret = append(ret, variableInput...)

	return ret, nil
}

func capitalise(input string) string {
	for len(input) > 0 && input[0] == '_' {
		input = input[1:]
	}
	if len(input) == 0 {
		return ""
	}
	return strings.ToUpper(input[:1]) + input[1:]
}

//go:build js && wasm

// The WebAssembly build: exposes rainward.plan(lotJSON, storm, budget) to the page's worker. The
// page passes strings and a number and gets a string back; no JavaScript objects cross the
// boundary, so nothing the page holds can be reached from here.
package main

import (
	"encoding/json"
	"errors"
	"strings"
	"syscall/js"

	"github.com/reihooneh/rainward/engine"
)

func reply(v any) js.Value {
	data, err := json.Marshal(v)
	if err != nil {
		return js.ValueOf(`{"error":"the result could not be encoded"}`)
	}
	return js.ValueOf(string(data))
}

func fail(msg string) js.Value { return reply(map[string]string{"error": msg}) }

func plan(_ js.Value, args []js.Value) (out any) {
	defer func() {
		if recover() != nil {
			out = fail("the planner hit an internal problem with this lot")
		}
	}()
	if len(args) != 3 || args[0].Type() != js.TypeString || args[1].Type() != js.TypeString || args[2].Type() != js.TypeNumber {
		return fail("plan takes a lot (JSON), a storm and a budget")
	}
	text := args[0].String()
	if len(text) > engine.MaxLotBytes {
		return fail("the lot is larger than 128 KB")
	}
	lot, err := engine.ParseLot(strings.NewReader(text))
	if err == nil {
		budget := args[2].Float()
		if !(budget >= 0 && budget <= 50000) {
			return fail("budget is 0 to 50000 dollars")
		}
		var res *engine.Result
		res, err = engine.Run(lot, engine.Options{Storm: args[1].String(), Budget: int(budget), Watch: true})
		if err == nil {
			return reply(res)
		}
	}
	var in *engine.InputError
	if errors.As(err, &in) {
		return fail(in.Msg)
	}
	return fail("the lot could not be planned")
}

func storms(_ js.Value, _ []js.Value) any { return reply(engine.Storms) }

func main() {
	js.Global().Set("rainward", js.ValueOf(map[string]any{
		"plan":   js.FuncOf(plan),
		"storms": js.FuncOf(storms),
		"ready":  true,
	}))
	select {} // keep the functions alive
}

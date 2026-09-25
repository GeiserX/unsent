//go:build race

package main

// The race detector slows the fuzz tests about tenfold: run fewer seeds.
const raceEnabled = true

//go:build !windows

package leftovers

func Bases() []string                           { return nil }
func Find(p Program, installed []string) Result { return Result{Program: p.Name} }

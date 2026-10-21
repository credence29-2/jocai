package programs

import (
	"sort"
	"strings"

	"github.com/credence29-2/jocai/cpc/internal/sysutil"
	"golang.org/x/sys/windows/registry"
)

const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall`

// List reads installed programs the same places Apps & Features does.
func List() []Program {
	type view struct {
		root registry.Key
		path string
		flag uint32
	}
	views := []view{
		{registry.LOCAL_MACHINE, uninstallKey, registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, uninstallKey, registry.WOW64_32KEY},
		{registry.CURRENT_USER, uninstallKey, 0},
	}
	var out []Program
	for _, v := range views {
		k, err := registry.OpenKey(v.root, v.path, registry.ENUMERATE_SUB_KEYS|v.flag)
		if err != nil {
			continue
		}
		subs, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, s := range subs {
			sk, err := registry.OpenKey(v.root, v.path+`\`+s, registry.QUERY_VALUE|v.flag)
			if err != nil {
				continue
			}
			str := func(n string) string { x, _, _ := sk.GetStringValue(n); return strings.TrimSpace(x) }
			num := func(n string) uint64 { x, _, _ := sk.GetIntegerValue(n); return x }
			p := Program{
				Name:        str("DisplayName"),
				Publisher:   str("Publisher"),
				Version:     str("DisplayVersion"),
				SizeKB:      int64(num("EstimatedSize")),
				InstallDate: ParseInstallDate(str("InstallDate")),
				Uninstall:   str("UninstallString"),
				Location:    str("InstallLocation"),
				Key:         s,
			}
			hidden := num("SystemComponent") == 1 || str("ParentKeyName") != "" || str("ReleaseType") == "Update"
			sk.Close()
			if p.Name == "" || p.Uninstall == "" || hidden {
				continue
			}
			out = append(out, p)
		}
	}
	out = Dedupe(out)
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// RunUninstaller starts the program's own uninstaller.
func RunUninstaller(p Program) error {
	return sysutil.RunCommandLine(UninstallCommand(p.Uninstall))
}

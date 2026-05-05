package render

import (
	"strings"

	"github.com/sugaf1204/gomi/bootenv/internal/spec"
)

func IPXEScript(doc spec.Document) string {
	var w strings.Builder
	cmdline := append([]string{}, doc.Spec.Cmdline...)
	cmdline = append(cmdline, doc.Spec.Initramfs.Cmdline...)
	cmdline = append(cmdline, doc.Spec.PXE.ExtraArgs...)
	cmdline = append(cmdline, rootfsBootArg(doc))

	w.WriteString("#!ipxe\n")
	w.WriteString("set base-url " + doc.Spec.PXE.BaseURL + "\n")
	w.WriteString("kernel ${base-url}/" + doc.Spec.PXE.KernelPath)
	if len(cmdline) > 0 {
		w.WriteByte(' ')
		w.WriteString(strings.Join(cmdline, " "))
	}
	w.WriteByte('\n')
	w.WriteString("initrd ${base-url}/" + doc.Spec.PXE.InitrdPath + "\n")
	w.WriteString("boot\n")
	return w.String()
}

func rootfsBootArg(doc spec.Document) string {
	rootfsURL := doc.Spec.PXE.BaseURL + "/" + doc.Spec.PXE.RootFSPath
	if doc.Spec.RootFS.Source.Type == "ubuntu-cloud-squashfs" {
		return "root=squash:" + rootfsURL
	}
	return "fetch=" + rootfsURL
}

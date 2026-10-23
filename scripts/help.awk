# Generate `make help` from annotations in the makefiles:
#   ##@ Section title|color     starts (or reopens) a section; color is green, yellow or red
#   target: prereqs ## text     documents a target; @GO@ @NODE@ @BIN@ expand to the
#                               Go version, Node version and binary directory
# Sections print in the order they are first declared (the root Makefile
# declares the order); entries print in makefile order within their section.
# Required -v: blue cyan green yellow red reset go node bin
BEGIN { FS = "\n" }
FNR == 1 { section = "" }
/^##@ / {
	split(substr($0, 5), part, "|")
	section = part[1]
	if (!(section in color)) {
		order[++count] = section
		color[section] = part[2]
	}
	next
}
section != "" && /^[A-Za-z0-9_$()<>.-]+:[^#]*## / {
	at = index($0, "## ")
	name = substr($0, 1, index($0, ":") - 1)
	if (name ~ /^\$\(/) name = "<target>-quiet"
	text = substr($0, at + 3)
	gsub("@GO@", go, text)
	gsub("@NODE@", node, text)
	gsub("@BIN@", bin, text)
	entries[section] = entries[section] sprintf("%s    make %-34s%s %s\n", hue(color[section]), name, reset, text)
}
function hue(name) {
	return name == "red" ? red : name == "yellow" ? yellow : green
}
END {
	printf "\n%s  Available commands:%s\n", blue, reset
	for (i = 1; i <= count; i++) {
		printf "\n%s  %s%s\n%s", cyan, order[i], reset, entries[order[i]]
	}
	printf "\n"
}

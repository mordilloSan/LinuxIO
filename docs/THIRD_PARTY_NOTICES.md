# Third-Party Notices

LinuxIO is licensed under the Apache License 2.0, distributed with it as
`LICENSE`. This file names the work of others that LinuxIO holds.

The sections down to "Source of the MPL-2.0 parts" are written by hand, in
`docs/THIRD_PARTY_NOTICES.md`. The copy a package installs in
`/usr/share/doc/linuxio/` goes on with what the build wrote from the programs
it built: Go's licence, the licence of every Go module linked into them, and
the licence of every package in the frontend bundle
(`packaging/scripts/third-party-notices.sh`).

## FileBrowser Quantum

Portions of the LinuxIO filesystem indexer are adapted from
[FileBrowser Quantum](https://github.com/gtsteffaniak/filebrowser).

Copyright 2018 File Browser contributors

FileBrowser Quantum and LinuxIO are licensed under the Apache License 2.0.
LinuxIO has modified the adapted portions. A copy of the Apache License 2.0 is
distributed with LinuxIO as `LICENSE`.

## Beszel

Portions of `linuxio-monitoring` derive from
[Beszel](https://github.com/henrygd/beszel) by henrygd.

MIT License

Copyright (c) 2024 henrygd

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## lot-map

The activity map on the LinuxIO Assistant page is adapted from
[lot-map](https://github.com/alexeyw/lot-map) by Aleksei Volodin: its
stations, icons, palette, wording and motion, and the rules by which it sorts
a call and a shell command line to a station, are that plugin's. LinuxIO has
modified the adapted portions.

MIT License

Copyright (c) 2026 Aleksei Volodin

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## Icon sets

The icons of the LinuxIO page are copied at build time from these sets,
through their [Iconify](https://iconify.design) packages, into the page
itself (`frontend/src/icons/icons.ts`):

- [Bootstrap Icons](https://github.com/twbs/icons) (`bi`), MIT License.
  Copyright (c) 2019-2024 The Bootstrap Authors
- [EOS Icons](https://gitlab.com/SUSE-UIUX/eos-icons) (`eos-icons`), MIT
  License. Copyright (c) 2020 SUSE Software Solutions Germany GmbH
- [Phosphor](https://github.com/phosphor-icons/core) (`ph`), MIT License.
  Copyright (c) 2023 Phosphor Icons
- [Line Awesome](https://github.com/icons8/line-awesome) (`la`), MIT License
  in its own repository, Apache License 2.0 in its Iconify package.
  Copyright (c) Icons8
- [Material Design Icons](https://github.com/Templarian/MaterialDesign)
  (`mdi`), by Pictogrammers, Apache License 2.0.
- [Simple Icons](https://github.com/simple-icons/simple-icons)
  (`simple-icons`), CC0 1.0 Universal, which asks for no notice. The brand
  icons remain trademarks of their owners.

For the four sets under the MIT License, with the copyright notices above:

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

The text of the Apache License 2.0 is LinuxIO's own `LICENSE`.

## Source of the MPL-2.0 parts

Two parts of LinuxIO are under the Mozilla Public License 2.0, whose text is
under their names further down. LinuxIO ships them unmodified, and their
source is available from their authors:

- `github.com/libp2p/go-yamux/v5`, linked into the Go programs:
  <https://github.com/libp2p/go-yamux>, at the version named under "Go
  modules".
- `@novnc/novnc`, in the frontend bundle: <https://github.com/novnc/noVNC>,
  at the version named under "Frontend packages".

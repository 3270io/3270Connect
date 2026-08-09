---
description: >-
  The projects 3270Connect is built on, starting with s3270 and the x3270
  family that carries out every workflow step.
---

# Acknowledgements

## s3270 and the x3270 family

3270Connect does not speak TN3270 itself. Every workflow step you write —
`Connect`, `FillString`, `PressEnter`, `CheckValue`, `AsciiScreenGrab` — is
carried out by **s3270**, the scripting member of the **x3270** family of 3270
terminal emulators. 3270Connect starts s3270, issues actions over its scripting
interface, and reads back the screen buffer; the pre-compiled binaries under
`binaries/` that ship inside every release are upstream x3270 builds.

Which is to say: the exacting part of this project — three decades of faithful
3270 and TN3270 protocol work, EBCDIC code pages, field attributes, structured
fields, TLS negotiation — was done by Paul Mattes and the x3270 contributors,
and given away for anyone to build on. 3270Connect is an automation layer
wrapped around their work, and it would not exist without it. Our thanks to
them.

If 3270Connect is useful to you, the upstream project is worth knowing about in
its own right:

- [x3270 project home and documentation](https://x3270.miraheze.org/wiki/Main_Page)
- [Source on GitHub](https://github.com/pmattes/x3270)
- [Licence (BSD 3-Clause)](https://github.com/pmattes/x3270/blob/master/LICENSE.md)

### Licence

s3270 is distributed under a BSD 3-Clause licence, copyright Paul Mattes, Don
Russell, Dick Altenbern, Jeff Sparkes and the Georgia Tech Research Corporation.
3270Connect uses it unmodified, as a separate executable.

Because 3270Connect redistributes those executables inside its own releases,
the full licence text is reproduced — as the licence requires of a binary
redistribution — in
[`THIRD-PARTY-LICENSES.md`](https://github.com/3270io/3270Connect/blob/main/THIRD-PARTY-LICENSES.md),
alongside a list of exactly which executables ship on which platform.

!!! note "Not an endorsement"

    The licence's third clause reserves the authors' names: they may not be
    used to endorse or promote products derived from the software. The x3270
    authors have no involvement in 3270Connect and have not reviewed, approved or
    promoted it. Naming them above states what this software uses, which the
    licence requires, and thanks them for it.

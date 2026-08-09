# Third-Party Notices

3270Connect embeds and redistributes software written by other people. This
file records what that software is, who wrote it, and the terms it comes under.

## s3270 — the x3270 family of 3270 terminal emulators

3270Connect does not implement TN3270 itself. Every workflow step — `Connect`,
`FillString`, `PressEnter`, `CheckValue`, `AsciiScreenGrab` — is carried out by
**s3270**, the scripting member of the **x3270** family of 3270 terminal
emulators. `connect3270/emulator.go` is a driver for it: it starts s3270,
issues actions over its scripting interface, and reads back the screen buffer.

That means the exacting part of this project — three decades of faithful 3270
and TN3270 protocol work, EBCDIC code pages, field attributes, structured
fields, TLS negotiation — was done by Paul Mattes and the x3270 contributors,
and given away for anyone to build on. 3270Connect exists because of it. Our
thanks to them.

- Project home and documentation: <https://x3270.miraheze.org/wiki/Main_Page>
- Source: <https://github.com/pmattes/x3270>
- Licence: <https://github.com/pmattes/x3270/blob/master/LICENSE.md>

### What 3270Connect redistributes

3270Connect ships pre-compiled x3270 executables in this repository, embedded
into the released binaries via `binaries/bindata.go` and extracted at runtime:

| Path | Executables |
|---|---|
| `binaries/linux/` | `s3270`, `x3270`, `x3270if` |
| `binaries/windows/` | `s3270.exe`, `ws3270.exe`, `wc3270.exe`, `x3270if.exe` |

The bundled build is s3270 4.1ga10. These executables are unmodified upstream
builds, produced by the `make linux` / `make windows` targets in the
[`Makefile`](Makefile). 3270Connect invokes them as separate processes; it does
not link against or embed x3270 source code.

Because 3270Connect redistributes these executables in binary form, the licence
below is reproduced in full, which is what the licence requires of a binary
redistribution.

### Licence — BSD 3-Clause

Reproduced verbatim from the x3270 distribution.

```
Copyright (c) 1993-2026 Paul Mattes.
Copyright (c) 2004-2005 Don Russell.
Copyright (c) 2004 Dick Altenbern.
Copyright (c) 1990 Jeff Sparkes.
Copyright (c) 1989 Georgia Tech Research Corporation (GTRC), Atlanta, GA
 30332.
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:
- Redistributions of source code must retain the above copyright
      notice, this list of conditions and the following disclaimer.
- Redistributions in binary form must reproduce the above copyright
      notice, this list of conditions and the following disclaimer in the
      documentation and/or other materials provided with the distribution.
- Neither the names of Paul Mattes, Don Russell, Dick Altenbern, Jeff
      Sparkes, GTRC nor the names of their contributors may be used to endorse
      or promote products derived from this software without specific prior
      written permission.

THIS SOFTWARE IS PROVIDED BY PAUL MATTES, DON RUSSELL, JEFF SPARKES, DICK
ALTENBERN AND GTRC "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING,
BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A
PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL PAUL MATTES, DON RUSSELL,
DICK ALTENBERN, JEFF SPARKES OR GTRC BE LIABLE FOR ANY DIRECT, INDIRECT,
INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE
OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF
ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Neither Paul Mattes, Don Russell, Dick Altenbern, Jeff Sparkes, GTRC, nor the
x3270 contributors endorse 3270Connect. The attribution above is exactly that —
attribution.

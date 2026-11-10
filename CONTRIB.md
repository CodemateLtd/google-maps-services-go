How to contribute
=================

Want to help out? That's awesome!

The library is open source and lives on GitHub at:
https://github.com/googlemaps/google-maps-services-go.
Open an issue or fork the library and submit a pull request.

Keep in mind that before we can accept any pull requests we have to jump
through a couple of legal hurdles, primarily a Contributor License Agreement
(CLA):

- **If you are an individual writing original source code**
  and you're sure you own the intellectual property,
  then you'll need to sign an
  [individual CLA](http://code.google.com/legal/individual-cla-v1.0.html).
- **If you work for a company that wants to allow you to contribute your work**,
  then you'll need to sign a
  [corporate CLA](http://code.google.com/legal/corporate-cla-v1.0.html)

Follow either of the two links above to access the appropriate CLA and
instructions for how to sign and return it. Once we receive it, we'll be able
to accept your pull requests.

Dependency maintenance
----------------------

The documented minimum remains Go 1.7. This dependency refresh does not change
that support policy. Go 1.8 is an additional compatibility-check target, not
an approved replacement for the documented minimum.

Keep the existing `go 1.14` module baseline unchanged, and avoid selecting
direct or transitive modules that declare a higher Go version. A dependency's
`go` directive alone does not establish source compatibility with the documented
minimum; review imported packages and build tags as well. Successful resolution
with a current Go toolchain does not prove compatibility with older releases.

The direct dependency selection is:

| Module | Selected version | Constraint |
| --- | --- | --- |
| `github.com/google/uuid` | `v1.6.0` | No module dependencies or declared Go version. |
| `github.com/kr/pretty` | `v0.3.0` | Uses `kr/text v0.2.0` and `go-internal v1.6.1` (Go 1.11), whose imported `fmtsort` package has a pre-Go-1.12 implementation. `v0.3.1` requires `go-internal v1.9.0` (Go 1.17), above the module baseline. |
| `github.com/sergi/go-diff` | `v1.3.0` | `v1.3.1` selects `yaml.v2 v2.4.0` (Go 1.15), above the module baseline. `v1.4.0` also introduces binary integer literals, requiring Go 1.13 source syntax. |
| `github.com/stretchr/testify` | `v1.6.1` (retained) | `v1.7.0` unconditionally calls `errors.Is`, `errors.As`, and `errors.Unwrap` in `assert`; these require Go 1.13. |
| `golang.org/x/time` | `v0.3.0` | `v0.4.0` begins declaring Go 1.18, above the module baseline; this is a declared-version policy boundary, not proof that its imported source fails on Go 1.8. |

The selected graph's declared Go versions are at most Go 1.14. In particular,
`pretty` and `go-diff` declare Go 1.12, but their imported source still needs
compatibility assessment against the documented Go 1.7 minimum. Passing a
Go 1.8 build alone would not establish Go 1.7 compatibility.
OpenCensus remains at `v0.22.3` and is maintained separately.

Sources: the published module files for
[uuid v1.6.0](https://proxy.golang.org/github.com/google/uuid/@v/v1.6.0.mod),
[pretty v0.3.0](https://proxy.golang.org/github.com/kr/pretty/@v/v0.3.0.mod),
[pretty v0.3.1](https://proxy.golang.org/github.com/kr/pretty/@v/v0.3.1.mod),
[go-internal v1.9.0](https://proxy.golang.org/github.com/rogpeppe/go-internal/@v/v1.9.0.mod),
[go-diff v1.3.1](https://proxy.golang.org/github.com/sergi/go-diff/@v/v1.3.1.mod),
[yaml.v2 v2.4.0](https://proxy.golang.org/gopkg.in/yaml.v2/@v/v2.4.0.mod),
and [x/time v0.4.0](https://proxy.golang.org/golang.org/x/time/@v/v0.4.0.mod);
the [go-diff v1.4.0 source](https://github.com/sergi/go-diff/blob/v1.4.0/diffmatchpatch/stringutil.go)
and [testify v1.7.0 source](https://github.com/stretchr/testify/blob/v1.7.0/assert/assertions.go).

The candidate graph was resolved with explicit versions and
`GOTOOLCHAIN=local go mod tidy -go=1.14`. Resolution and source review are
not compatibility test results. Before accepting dependency changes, assess
Go 1.7 compatibility and report existing baseline failures separately. Also run
the full build and tests with Go 1.8 as an additional check, module-mode controls
with Go 1.14, and a current Go release, including examples and race-enabled tests
where supported. Exercise rate-limiter scheduling, deadline rejection, and
cancellation without live API calls, and review a vulnerability scan of the
resolved graph separately; dependency age
alone does not establish a vulnerability.

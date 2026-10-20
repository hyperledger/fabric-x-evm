# Dependency Notes

This document records the third-party Go dependencies that the October 2026
dependency scan flagged as archived, unmaintained, vulnerable or license-sensitive,
and why each one is still here. Re-check it when bumping go-ethereum,
fabric-x-committer, fabric-x-common or fabric-lib-go, since those bring in almost
all of the flagged modules.

"Binary" means the module is linked into `fxevm` (per `go version -m`). "Tools/tests"
means it is only used by `go tool` helpers (abigen, cryptogen, configtxgen, fxconfig,
addlicense, staticcheck) or by tests, and does not ship in the release image.

## Direct dependencies

All direct dependencies are on their latest stable release, except:

| Module | Version | Reason |
|---|---|---|
| `github.com/hyperledger/fabric-x-common` | pre-v0.2.9 pseudo-version | v0.2.9+ removed the `committerpb` query/notifier services that `fabric-x-sdk` still uses. Pinned to the minimum `fabric-x-committer` v1.0.5 requires. Move to a tag once `fabric-x-sdk` and a committer release use `SidecarService`. |
| `github.com/hyperledger/fabric-x-sdk` | pseudo-version | No tagged release contains the APIs we use. |

## Archived upstream

None of these are direct dependencies; all come in through go-ethereum and cannot be
replaced from this repository.

| Module | Via | Ships in |
|---|---|---|
| `github.com/mitchellh/mapstructure` | go-ethereum (`cmd/utils`, `fastssz`) | binary |
| `github.com/mitchellh/pointerstructure` | go-ethereum `node` → `hashicorp/go-bexpr` | binary |
| `github.com/StackExchange/wmi` | go-ethereum `metrics` → `shirou/gopsutil` (Windows only) | tools/tests |
| `github.com/opentracing/opentracing-go` | go-ethereum `graphql` → `graph-gophers/graphql-go` | tools/tests |

## Unmaintained upstream

All transitive. Grouped by the direct dependency that brings them in.

| Via | Modules |
|---|---|
| go-ethereum | `gorilla/websocket`, `syndtr/goleveldb`, `huin/goupnp`, `ferranbt/fastssz`, `crate-crypto/go-eth-kzg`, `ethereum/go-bigmodexpfix`, `holiman/bloomfilter/v2`, `minio/sha256-simd`, `cockroachdb/fifo`, `davecgh/go-spew`, `go-logr/stdr`, `natefinch/lumberjack.v2`; tools/tests only: `gballet/go-libpcsclite`, `peterh/liner`, `holiman/billy`, `influxdata/influxdb1-client`, `influxdata/line-protocol`, `go-ole/go-ole`, `dchest/siphash`, `clipperhouse/uax29/v2` |
| pebble (via go-ethereum and ours) | `cockroachdb/redact`, `cockroachdb/tokenbucket`, `gogo/protobuf`, `kr/pretty`, `kr/text` |
| fabric-x-committer | `yugabyte/pgx/v5`, `jackc/pgpassfile`, `jackc/pgservicefile`, `cockroachdb/logtags` |
| fabric-lib-go | `pkg/errors`, `go-logfmt/logfmt`, `go.uber.org/multierr` |
| fabric-x-common | `go-viper/mapstructure/v2`; tools/tests only: `hyperledger/fabric-amcl` |
| fabric-x tools (configtxgen, cryptogen) | tools/tests only: `spf13/viper`, `sagikazarmark/locafero`, `subosito/gotenv`, `alecthomas/units` |
| prometheus/client_golang | `beorn7/perks`, `cespare/xxhash/v2`, `munnerz/goautoneg` |
| modernc.org/sqlite | `google/uuid`, `remyoudompheng/bigfft`, `modernc.org/mathutil` |
| urfave/cli (go-ethereum) | `cpuguy83/go-md2man/v2`, `russross/blackfriday/v2`, `xrash/smetrics` |
| spf13/cobra | tools/tests only: `inconshreveable/mousetrap` |
| (tool) | `google/addlicense`: used only by `make checks` |

`gorilla/websocket` is also imported directly by `gateway/api/server_ws_test.go`;
go-ethereum's `rpc` package uses it for the WebSocket server, so dropping it from the
test would not remove it from the build.

Several of these are "unmaintained" only in the sense of having no recent commits
(for example `cespare/xxhash`, `google/uuid`, `beorn7/perks`); they are stable,
widely used and have no known vulnerabilities.

## Vulnerabilities

`govulncheck ./...` reports no vulnerabilities reachable from our code.

It reports one finding in a required module that we do not call:
GO-2026-5932, `golang.org/x/crypto/openpgp` is deprecated. We only use
`golang.org/x/crypto/sha3`; there is no fixed version.

## Licenses

| Module | License | Notes |
|---|---|---|
| `github.com/ethereum/go-ethereum` | LGPL-3.0-or-later | The Go code in this repository is itself LGPL-3.0-or-later (see `LICENSE.LGPL3`). |
| `github.com/gballet/go-libpcsclite` | BSD-3-Clause | The scan reports LGPL-2.1-or-later, but the module's LICENSE file is BSD-3-Clause. Tools/tests only, not in the release binary. |
| `github.com/hashicorp/go-bexpr` | MPL-2.0 | Via go-ethereum `node`; file-level copyleft, unmodified. |

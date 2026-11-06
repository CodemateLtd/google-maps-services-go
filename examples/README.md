# Command-line examples

Run these commands from the repository root, using a Go toolchain compatible
with [`go.mod`](../go.mod). See the [root setup guidance](../README.md#requirements)
for project prerequisites, then download this checkout's dependencies:

```sh
go mod download
```

## Project, billing and credentials

- Use a Google Cloud project with billing enabled. Enable the API listed below
  for the command you need, and create an API key in that project. See
  [Google Maps Platform setup](https://developers.google.com/maps/get-started).
- Restrict the key to the required APIs and the server's public IP addresses,
  following the [API security guidance](https://developers.google.com/maps/api-security-best-practices#restricting-api-keys).
  Keep real keys and signing secrets out of source control and shared logs.
- Set `GOOGLE_MAPS_API_KEY` securely in your shell. The commands explicitly pass
  it through `-key`; they do not read environment variables or `.env` files
  themselves. Flags can be visible in process listings, so use a trusted machine.
- Running an example can incur service charges. Run only the example you need,
  not this entire list. The Place Photo example can also create a local file.
- The client-configuration example's explicit `-local` mode is an exception:
  it uses only a loopback fixture and fictional credentials, without API setup.

The key is the default credential for every command. All seven Places commands
and both Roads commands require an API key. The former `-client_id` and
`-signature` flags have been removed from the six Places commands that offered
that unsupported authentication path; Find Place was already key-only.
Directions, Distance Matrix, Elevation, Geocoding, Time Zone and Static Maps retain
client-ID authentication only for [eligible existing Premium Plan customers](https://developers.google.com/maps/premium/overview).
Premium Plan enrollment is closed; this is not new-project setup guidance.

## Available commands

Directions, Distance Matrix and Places below use **Legacy** APIs. Existing eligible
projects remain supported; Legacy does not mean deprecated or scheduled for
shutdown. Consult the [current availability policy](https://developers.google.com/maps/legacy)
and the [root lifecycle notice](../README.md) before enabling services.
Enabling Places API (New) or Routes API does not enable these Legacy examples.
For newer integrations, use the external [Go Places](https://github.com/googleapis/google-cloud-go/tree/main/maps/places)
and [Go Routes](https://github.com/googleapis/google-cloud-go/tree/main/maps/routing)
clients, with the [Places](https://developers.google.com/maps/documentation/places/web-service/legacy/migrate-overview)
or [Routes](https://developers.google.com/maps/documentation/routes/migrate-routes)
migration guide. Newer APIs will not be added to this SDK.

| Command | Root-relative package | API to enable |
| --- | --- | --- |
| Directions | `./examples/directions/cmdline` | Directions API (Legacy) |
| Distance Matrix | `./examples/distancematrix/cmdline` | Distance Matrix API (Legacy) |
| Elevation | `./examples/elevation/cmdline` | Elevation API |
| Geocoding | `./examples/geocoding/cmdline` | Geocoding API |
| Find Place from Text | `./examples/places/findplacefromtext` | Places API (Legacy) |
| Nearby Search | `./examples/places/nearbysearch` | Places API (Legacy) |
| Place Photo | `./examples/places/photo` | Places API (Legacy) |
| Place Autocomplete | `./examples/places/placeautocomplete` | Places API (Legacy) |
| Place Details | `./examples/places/placedetails` | Places API (Legacy) |
| Query Autocomplete | `./examples/places/queryautocomplete` | Places API (Legacy) |
| Text Search | `./examples/places/textsearch` | Places API (Legacy) |
| Snap to Roads | `./examples/roads/snaptoroad` | Roads API |
| Speed Limits | `./examples/roads/speedlimits` | Roads API; eligible Asset Tracking access required |
| Static Maps | `./examples/staticmap/cmdline` | Maps Static API |
| Time Zone | `./examples/timezone/cmdline` | Time Zone API |
| Client configuration | `./examples/clientconfig/cmdline` | Geocoding API for real requests; none for `-local` |

[Speed Limits access](https://developers.google.com/maps/documentation/roads/speed-limits)
is restricted; enabling Roads API alone does not grant it.
Most commands print pretty-formatted Go SDK values, not JSON. Photo logs its
content type and image bounds and writes `<basename>.jpg` when `-basename` is set.
Static Maps prints the decoded Go image value; it does not save an image file.

## Invocation examples

Set the shell variables `PLACE_ID`, `PHOTO_REFERENCE` and `ROAD_PLACE_IDS` to
values from actual service results. Photo references come from Places photo
metadata; road segment place IDs come from Roads results, not business place IDs.
Quote spaces, commas and especially `|` delimiters so the shell does not treat
arguments as pipelines. Time Zone's timestamp is Unix time in seconds.
Use the same package with `-h` to inspect its flags without making a request.

```sh
# Directions: origin and destination.
go run ./examples/directions/cmdline -key "$GOOGLE_MAPS_API_KEY" -origin 'Sydney, Australia' -destination 'Melbourne, Australia'

# Distance Matrix: pipe-separated origins and destinations.
go run ./examples/distancematrix/cmdline -key "$GOOGLE_MAPS_API_KEY" -origins 'Sydney, Australia|Canberra, Australia' -destinations 'Melbourne, Australia'

# Elevation: one or more latitude,longitude pairs.
go run ./examples/elevation/cmdline -key "$GOOGLE_MAPS_API_KEY" -locations '-33.8688,151.2093|-35.2809,149.1300'

# Geocoding: address lookup.
go run ./examples/geocoding/cmdline -key "$GOOGLE_MAPS_API_KEY" -address 'Sydney Opera House'

# Find Place: input type is required; request only the fields you need.
go run ./examples/places/findplacefromtext -key "$GOOGLE_MAPS_API_KEY" -input 'Sydney Opera House' -inputtype textquery -fields 'name,place_id'

# Nearby Search: location and radius in meters.
go run ./examples/places/nearbysearch -key "$GOOGLE_MAPS_API_KEY" -location '-33.8688,151.2093' -radius 1000

# Place Photo: reference and at least one dimension; writes place-photo.jpg.
go run ./examples/places/photo -key "$GOOGLE_MAPS_API_KEY" -photoreference "$PHOTO_REFERENCE" -maxwidth 400 -basename place-photo

# Place Autocomplete: place/address predictions.
go run ./examples/places/placeautocomplete -key "$GOOGLE_MAPS_API_KEY" -input 'Sydney Ope'

# Place Details: place ID and selected fields.
go run ./examples/places/placedetails -key "$GOOGLE_MAPS_API_KEY" -place_id "$PLACE_ID" -fields 'name,formatted_address'

# Query Autocomplete: query predictions.
go run ./examples/places/queryautocomplete -key "$GOOGLE_MAPS_API_KEY" -input 'pizza near Sydney'

# Text Search: textual query.
go run ./examples/places/textsearch -key "$GOOGLE_MAPS_API_KEY" -query 'restaurants in Sydney'

# Snap to Roads: pipe-separated path coordinates; interpolation is optional.
go run ./examples/roads/snaptoroad -key "$GOOGLE_MAPS_API_KEY" -path '-35.27801,149.12958|-35.28032,149.12907' -interpolate

# Speed Limits: comma-separated road segment place IDs, or use -path instead.
go run ./examples/roads/speedlimits -key "$GOOGLE_MAPS_API_KEY" -place_ids "$ROAD_PLACE_IDS" -units KPH

# Static Maps: explicit center, zoom and pixel dimensions.
go run ./examples/staticmap/cmdline -key "$GOOGLE_MAPS_API_KEY" -center 'Sydney, Australia' -zoom 12 -size 400x400

# Time Zone: location and a Unix timestamp.
go run ./examples/timezone/cmdline -key "$GOOGLE_MAPS_API_KEY" -location '-33.8688,151.2093' -timestamp 1735689600
```

### Retained signing and legacy credentials

Static Maps can sign an API-key request with `-signature`. Supply the URL-safe
Base64 **URL signing secret**, not a precomputed request signature. Signing may be
required for your usage and is recommended by the [digital-signature guide](https://developers.google.com/maps/documentation/maps-static/digital-signature).

```sh
go run ./examples/staticmap/cmdline -key "$GOOGLE_MAPS_API_KEY" -signature "$GOOGLE_MAPS_SIGNING_SECRET" -center 'Sydney, Australia' -zoom 12 -size 400x400
```

For an eligible existing client ID, omit `-key` and supply both credentials, for
example:

```sh
go run ./examples/geocoding/cmdline -client_id "$GOOGLE_MAPS_CLIENT_ID" -signature "$GOOGLE_MAPS_SIGNING_SECRET" -address 'Sydney Opera House'
```

Do not combine API keys and client IDs: the examples prefer `-key`, not a merged
credential set. Static Maps uses a supplied signing secret with that key;
the other retained examples use it only with the legacy client ID. Without an API
key, legacy authentication requires both a client ID and its signing secret.

## Client configuration and request context

Run the complete configuration example offline:

```sh
go run ./examples/clientconfig/cmdline -local -channel 123
```

The explicit local mode uses an in-process HTTP fixture, fictional credentials
and `WithBaseURL`; it makes no Google request and accepts no arbitrary endpoint.
For a real request, enable Geocoding API and billing and supply authorized input:

```sh
go run ./examples/clientconfig/cmdline -key "$GOOGLE_MAPS_API_KEY" -address '1600 Amphitheatre Parkway, Mountain View, CA' -channel 123
```

This example owns its HTTP client and transport, with a 5-second HTTP timeout and
a 3-second request-context deadline by default (`-http_timeout` and
`-request_timeout`). It defers cancellation and combines client experience IDs
with appended request-context IDs rather than mutating shared client metadata.
Geocoding supports numeric channels 0–999 with API keys; consult the
[channel support list](https://developers.google.com/maps/reporting-and-monitoring/reporting#usage-tracking-per-channel)
before applying the option to another API.

Results go to stdout. The existing `metrics.Reporter` callback supplies the
service path, HTTP status and transport/JSON error presence to a credential-safe
stderr summary; API-status errors are checked later by the SDK and may not appear
as callback errors. The command still fails on those errors. The reporter is for
one sequential call; concurrent reuse needs synchronized storage or a safe sink.

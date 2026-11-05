// Copyright 2015 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
Package maps provides a client library for the Google Maps Web Service APIs.
Please see https://developers.google.com/maps/documentation/webservices/ for
an overview of the Maps Web Service API suite.

This library supports Elevation, Geocoding, Roads, Time Zone and Static Maps,
as well as Directions API (Legacy), Distance Matrix API (Legacy) and Places
API (Legacy). Existing Legacy callers remain supported; Legacy status does
not mean deprecated. See https://developers.google.com/maps/legacy.

For new integrations, newer APIs are available through the external
google-cloud-go/maps client families:

  - Places API (New): https://github.com/googleapis/google-cloud-go/tree/main/maps/places
  - Routes API: https://github.com/googleapis/google-cloud-go/tree/main/maps/routing

Newer APIs will not be added to this SDK. For migration guidance from
Directions and Distance Matrix to Routes API, see
https://developers.google.com/maps/documentation/routes/migrate-routes.
For Places API (New), see
https://developers.google.com/maps/documentation/places/web-service/legacy/migrate-overview.
*/
package maps // import "googlemaps.github.io/maps"

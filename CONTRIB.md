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

Testing
-------

CI checks Go 1.8.7 in GOPATH/vendor mode and current stable Go in module mode.
The stable job retains race detection and coverage reporting. See the
[legacy installation instructions](README.md#installation) to reproduce the
minimum check. Old Go's recursive `./...` also visits vendor packages, so the
minimum job explicitly selects this repository's packages.

Keep dependency changes compatible with this minimum, or make an explicit
support-policy decision. Public-branch Codecov uploads require tokenless uploads
to be enabled in the organization's settings; fork PR uploads remain secretless.

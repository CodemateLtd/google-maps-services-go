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

Automation
----------

CI uses GitHub-hosted `ubuntu-latest` runners. Releases provision Node 24
(24.15 or later) and run the pinned semantic-release engine and plugins
listed in `.github/workflows/release.yml`. These are release-tool requirements,
not changes to the library's supported Go versions.

Releases remain master-only, use `SYNCED_GITHUB_TOKEN_REPO`, and load
`.releaserc` without npm publishing. Coverage uploads retain tokenless
authentication; public-repository tokenless uploads must be enabled in
the Codecov organization's settings.

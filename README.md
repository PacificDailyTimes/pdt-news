# pdt-news

A news CMS in Go. Not WordPress. Path-only **multiauthor** when you want a paper plus author blogs; a single-author blog when you don’t.

GPLv3. PostgreSQL. Nginx in front of `127.0.0.1:9001`.

## Two modes (`mode=` in config)

| | single | network |
|---|---|---|
| Front | `domain.tld` is the blog | `domain.tld` is the paper (aggregator) |
| Bio | — | `domain.tld/@handle` |
| Author blog | — | `domain.tld/handle` |
| Login | **login ID**, never a URL | same |

Do not call this “multisite” or “multiauthor”. It is a **network**. Subdomain author blogs wait on inkcert.

## People

- **Admin** — paper, sites, wallet, aggregator, products
- **Author** — compose, bio, media. Public **handle** only if they want one
- **Consumer** — shop, follow authors, read in-house feeds. No public username. No comments.

Login ID ≠ handle. Login ID never prints on the public site.

## Content

- **Piece** (post) and **page** (menus)
- **Series** (501 taxonomy) with `/series/slug`, `/rss`, `/atom`, `/feed`
- Featured image / audio / video at top or bottom (podcast enclosure)
- **Product**: physical, virtual, or a simple subscription (`every N day|week|month|year`, start date, skip-until)
- RSS importer (aggregator)

## Reader

`Aa` control: white / black / dark / gray / beige; Newsreader (old-style), Source Serif 4 (transitional), Source Sans 3; size; newspaper columns off or N-per-16:9-screen (scales with window width); copy link.

Fonts are OFL and vendored in `web/static/fonts/`.

## Auth

Email **code + link** (default). Optional password. Authenticator (TOTP). Installer is **passwordless** unless you set `setup_password=` (plaintext) or `setup_password_hash=` (sha256 hex) in config.

## Money

Stripe / PayPal keys in config or the admin dash. US destination tax from a state table. Invoices as a short email + PDF. Crypto addresses and **key files only in `/etc/pdt/wallet/`** (0700). Tarball installs still use that FHS path for keys.

## Install

```
go build -o pdt ./cmd/pdt
./pdt
# open /install
```

Packages (Arch / Debian / RPM): `pack/`. Post-install:

```
sudo bash /usr/share/pdt/contrib/pdt-install --url https://example.tld --db-name pdt --db-user pdt --db-pass secret
# or --interactive  (still no setup-password prompt)
```

Config lives at `/etc/pdt/config`, symlinked from `/srv/www/pdt/config` (or `/var/www/pdt`). Tarball: `config` next to the binary.

Nginx sample: `contrib/nginx/pdt.conf`.

## write.pink

Colored words (editor color + dashboard CSS classes), featured media, series feeds, shop links, a reading surface that does not fight the page — those behaviors live here without cloning that theme.

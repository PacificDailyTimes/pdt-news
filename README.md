# pdt-news

A news CMS in Go. Not WordPress. Path-only **network** when you want a paper plus author blogs; a single-author blog when you don’t.

GPLv3. PostgreSQL. Nginx in front of `127.0.0.1:9001`.

One compose family, one shop family, one site family — not pages over here and WooCommerce over there.

## Two modes (`mode=` in config)

| | single | network |
|---|---|---|
| Front | `domain.tld` is the blog | `domain.tld` is the paper (aggregator) |
| Bio | — | `domain.tld/@handle` |
| Author blog | — | `domain.tld/handle` |
| Login | **login ID**, never a URL | same |

Subdomain author blogs wait on inkcert.

## inkMail (enterprise)

On a Verber, `ink install inkmailadmin` is the postfix-maddy agnostic mail panel. For a pdt site, set `domain_lock=` to the paper domain in `/etc/inkmail/conf` so every subdomain of that paper gets an inbox or alias when created. BIMI is `https://${emailTLDURI}/domain.tld/bimi.svg` via `ink set bimi`.

## People


- **Admin** — paper, site look, SEO, menus, wallet, aggregator, shop
- **Author** — compose, bio, media. Public **handle** only if they want one
- **Consumer** — cart, follow authors, bookings, invoices. No public username. No comments.

Login ID ≠ handle. Login ID never prints on the public site.

## Content (one editor)

| Type | What it is |
|---|---|
| **Piece** | Post. A piece can be a podcast episode (featured audio). |
| **Page** | Same editor, not a different app. |
| **Landing** | 1–7 CTA columns. Each column: header, button, logo, text, background (flat / gradient / image). Count is AJAX like 501 series. |
| **Contact** | CF7-shaped form. Fields in one list. `/contact/{slug}` |
| **Scroll-thru** | Stack of pages / landings / contacts. URL changes as you scroll: `/{scroll}/welcome` → `/{scroll}/tiers`. Network: `/{handle}/{scroll}/{item}` |
| **Series** | 501 taxonomy. `/series/slug`, `/rss`, `/atom`, `/feed` |

Every type has a **min tier** (0 = public). Higher membership ranks inherit lower, like Patreon / Paid Memberships Pro.

## Shop

| Type | URL |
|---|---|
| Product | `/shop/{slug}` |
| Department (product series) | `/shop/d/{slug}` |
| Product index | `/shop/i/{slug}` |
| Cart | `/cart` — qty, save for later, favorite, remove |

Two subscriptions, on purpose:

1. **Membership** — access rank (does not ship)
2. **Product subscription** — ship / access on a beat (`every N day\|week\|month\|year`, start, skip-until)

Product **features** are one shape: name + `menu` (Amazon swatches: selected, gray, slash if unavailable) or `select` (dropdown). Color, size, material, style — same input.

A membership can sit on a landing column next to a product. Same checkout.

## Calendar

Site-wide word: **appointment** or **reservation**. Slot size + weekday windows.

CalDAV is a **client**, like iOS Calendar or DAVx⁵. Connect **Apple iCloud** (Apple ID + app password, we discover the collection), **Google Calendar** (OAuth, `oauth_google_id` in config), or **CalDAV** (Nextcloud/ownCloud/Fastmail). Dash agenda can add/delete events on that host. Bookings PUT onto it; phone moves/deletes come back on view or Sync. We do not host CalDAV. Subscribe feed remains `/cal/{slug}.ics`.




## Site

Dash → Site: look (round/square corners), SEO (title, description, image, robots — same idea as 501 `in.head.php`), social (site-wide; authors have their own on Bio), menus (up/down, places above/below post, page, landing, series, shop, department, product), feature toggles, payment keys.

**SysAdmin config wins.** If `stripe_secret=` is in `/etc/pdt/config`, the dashboard key fields lock. If it is empty, a friendly single-blog admin fills them in the dash. Feature flags: empty = site admin may toggle; `0`/`1` locks them.

badAd is the other way around: keys live only in config. That product is a wide network with a powerful SysAdmin.

## Money

Stripe / PayPal: one-time **or** auto-renewing memberships/subscriptions. Crypto: prepaid, **never** renews. US destination tax. Invoices email + PDF. Crypto keys only in `/etc/pdt/wallet/` (0700).

## Reader

`Aa`: white / black / dark / gray / beige; Newsreader / Source Serif 4 / Source Sans 3; size; newspaper columns; copy link. Site admin picks a default light/dark; the visitor’s Aa still wins.

## Themes

Layout is `web/static/css/pdt.css` (mast, nav, cards, thread). A theme is one file in `web/static/css/themes/{name}.css`. Drop a file, it appears in Dash → Site. The file may only set CSS variables and the `theme-` hooks (`.theme-cta`, `.theme-mark`). No new navigation. Visitors already know header / feed / post.

Shipped: `masthead`, `night`, `ink`, `paper`, `wire`, `markets`.


## Auth

Email code + link. Optional password. Authenticator. Google / Apple / GitHub. Installer is passwordless unless `setup_password=` or `setup_password_hash=`.

## Install

```
go build -o pdt ./cmd/pdt
./pdt
# open /install
```

Packages: [pdt-news-package](https://github.com/PacificDailyTimes/pdt-news-package) (Arch/Debian/RPM). Config: `/etc/pdt/config`, symlinked from `/srv/www/pdt/config`. Nginx: `contrib/nginx/pdt.conf`. Installer: `contrib/pdt-install` (`--webroot` sets destination; interactive does not ask for it).

On Verb the machine name is `vapps/pdt.DOMAIN.TLD`; the public host is the domain itself. BIMI is served at `https://domain.tld/bimi.svg`.

-- pdt-news PostgreSQL schema
CREATE TABLE IF NOT EXISTS meta (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  login_id VARCHAR(64) NOT NULL UNIQUE,
  email VARCHAR(160) NOT NULL UNIQUE,
  pass_hash TEXT,
  role TEXT NOT NULL DEFAULT 'consumer',
  handle VARCHAR(64) UNIQUE,
  name TEXT NOT NULL DEFAULT '',
  bio TEXT NOT NULL DEFAULT '',
  avatar TEXT,
  totp_secret TEXT,
  totp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS users_role ON users(role);

CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS magic_links (
  token TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS passkeys (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  cred_id BYTEA NOT NULL UNIQUE,
  pubkey TEXT NOT NULL,
  name TEXT,
  sign_count INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sites (
  id BIGSERIAL PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  tagline TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  theme TEXT NOT NULL DEFAULT 'masthead',
  colors JSONB NOT NULL DEFAULT '{}',
  is_main BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS site_members (
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  can_post BOOLEAN NOT NULL DEFAULT TRUE,
  PRIMARY KEY (site_id, user_id)
);

CREATE TABLE IF NOT EXISTS series (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  descr TEXT NOT NULL DEFAULT '',
  lang TEXT NOT NULL DEFAULT 'en',
  author TEXT,
  owner TEXT,
  email TEXT,
  copy TEXT,
  keywords TEXT,
  explicit BOOLEAN NOT NULL DEFAULT FALSE,
  itunes_cat TEXT,
  UNIQUE (site_id, slug)
);

CREATE TABLE IF NOT EXISTS pieces (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  author_id BIGINT NOT NULL REFERENCES users(id),
  type TEXT NOT NULL DEFAULT 'post',
  status TEXT NOT NULL DEFAULT 'draft',
  series_id BIGINT REFERENCES series(id),
  title TEXT NOT NULL,
  slug TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  excerpt TEXT NOT NULL DEFAULT '',
  feat_img TEXT,
  feat_aud TEXT,
  feat_vid TEXT,
  feat_place TEXT NOT NULL DEFAULT 'top',
  published_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (site_id, slug)
);
CREATE INDEX IF NOT EXISTS pieces_live ON pieces(site_id, type, status, published_at DESC);

CREATE TABLE IF NOT EXISTS menus (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  href TEXT NOT NULL,
  pos INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS media (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT REFERENCES sites(id) ON DELETE SET NULL,
  user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  kind TEXT NOT NULL,
  path TEXT NOT NULL,
  title TEXT,
  mime TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS aggregation (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  source TEXT NOT NULL,
  series_id BIGINT REFERENCES series(id),
  interval_min INT NOT NULL DEFAULT 15,
  status TEXT NOT NULL DEFAULT 'active',
  last_fetch TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS style_classes (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  css TEXT NOT NULL,
  UNIQUE (site_id, name)
);

CREATE TABLE IF NOT EXISTS products (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  slug TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  price_cents INT NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'USD',
  kind TEXT NOT NULL DEFAULT 'physical',
  virtual_url TEXT,
  interval_n INT,
  interval_unit TEXT,
  ship BOOLEAN NOT NULL DEFAULT FALSE,
  weight_g INT,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  UNIQUE (site_id, slug)
);

CREATE TABLE IF NOT EXISTS orders (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT REFERENCES users(id),
  email TEXT,
  total_cents INT NOT NULL,
  tax_cents INT NOT NULL DEFAULT 0,
  ship_cents INT NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'USD',
  status TEXT NOT NULL DEFAULT 'pending',
  dest_country TEXT,
  dest_state TEXT,
  dest_zip TEXT,
  dest_addr TEXT,
  pay_via TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS order_items (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  product_id BIGINT REFERENCES products(id),
  title TEXT NOT NULL,
  qty INT NOT NULL DEFAULT 1,
  price_cents INT NOT NULL
);

CREATE TABLE IF NOT EXISTS invoices (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  number TEXT NOT NULL UNIQUE,
  pdf_path TEXT,
  emailed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS subscriptions (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  product_id BIGINT NOT NULL REFERENCES products(id),
  status TEXT NOT NULL DEFAULT 'active',
  interval_n INT NOT NULL,
  interval_unit TEXT NOT NULL,
  start_on DATE NOT NULL,
  skip_until DATE,
  next_on DATE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS follows (
  consumer_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  author_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (consumer_id, author_id)
);

CREATE TABLE IF NOT EXISTS entitlements (
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  product_id BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  source TEXT NOT NULL DEFAULT 'purchase',
  PRIMARY KEY (user_id, product_id)
);

CREATE TABLE IF NOT EXISTS oauth_identities (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  subject TEXT NOT NULL,
  email TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (provider, subject)
);

CREATE TABLE IF NOT EXISTS oauth_codes (
  code TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  client_id TEXT NOT NULL,
  redirect TEXT NOT NULL,
  expires TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS oauth_tokens (
  token TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  client_id TEXT NOT NULL,
  expires TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS badad_links (
  user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  badad_user TEXT,
  pub_key TEXT,
  sec_key TEXT,
  linked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- payments (additive)
CREATE TABLE IF NOT EXISTS crypto_payments (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT REFERENCES orders(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'pending',
  note TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE orders ADD COLUMN IF NOT EXISTS provider_ref TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS renew BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS period_end TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS kind TEXT;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS pay_via TEXT;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS renew BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS expires_on DATE;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS provider_sub TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS member_until TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS social JSONB NOT NULL DEFAULT '{}';

-- site look / SEO
ALTER TABLE sites ADD COLUMN IF NOT EXISTS corners TEXT NOT NULL DEFAULT 'square';
ALTER TABLE sites ADD COLUMN IF NOT EXISTS social JSONB NOT NULL DEFAULT '{}';
ALTER TABLE sites ADD COLUMN IF NOT EXISTS seo_title TEXT;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS seo_desc TEXT;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS seo_image TEXT;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS book_word TEXT NOT NULL DEFAULT 'appointment';
ALTER TABLE sites ADD COLUMN IF NOT EXISTS robots TEXT NOT NULL DEFAULT 'index,follow';

-- piece extras: landing JSON, SEO, tiers
ALTER TABLE pieces ADD COLUMN IF NOT EXISTS min_tier INT NOT NULL DEFAULT 0;
ALTER TABLE pieces ADD COLUMN IF NOT EXISTS seo_title TEXT;
ALTER TABLE pieces ADD COLUMN IF NOT EXISTS seo_desc TEXT;
ALTER TABLE pieces ADD COLUMN IF NOT EXISTS seo_image TEXT;
ALTER TABLE pieces ADD COLUMN IF NOT EXISTS landing JSONB;
ALTER TABLE pieces ADD COLUMN IF NOT EXISTS robots TEXT NOT NULL DEFAULT 'index,follow';

ALTER TABLE products ADD COLUMN IF NOT EXISTS department_id BIGINT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS min_tier INT NOT NULL DEFAULT 0;
ALTER TABLE products ADD COLUMN IF NOT EXISTS stock INT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS features JSONB NOT NULL DEFAULT '[]';
ALTER TABLE products ADD COLUMN IF NOT EXISTS seo_title TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS seo_desc TEXT;

ALTER TABLE series ADD COLUMN IF NOT EXISTS min_tier INT NOT NULL DEFAULT 0;
ALTER TABLE series ADD COLUMN IF NOT EXISTS seo_title TEXT;
ALTER TABLE series ADD COLUMN IF NOT EXISTS seo_desc TEXT;
ALTER TABLE series ADD COLUMN IF NOT EXISTS seo_image TEXT;

-- site settings (dashboard). Config file wins and locks when set.
CREATE TABLE IF NOT EXISTS settings (
  site_id BIGINT NOT NULL DEFAULT 1,
  k TEXT NOT NULL,
  v TEXT NOT NULL,
  PRIMARY KEY (site_id, k)
);

-- membership ranks. 0 is public. Higher rank inherits lower (Patreon-style).
CREATE TABLE IF NOT EXISTS tiers (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  rank INT NOT NULL DEFAULT 1,
  product_id BIGINT REFERENCES products(id) ON DELETE SET NULL,
  UNIQUE (site_id, slug)
);

-- contact forms (CF7-shaped, one table of fields as JSON)
CREATE TABLE IF NOT EXISTS forms (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  fields JSONB NOT NULL DEFAULT '[{"name":"name","label":"Name","type":"text","required":true},{"name":"email","label":"Email","type":"email","required":true},{"name":"message","label":"Message","type":"textarea","required":true}]',
  notify TEXT,
  thanks TEXT NOT NULL DEFAULT 'Sent. We will reply.',
  min_tier INT NOT NULL DEFAULT 0,
  UNIQUE (site_id, slug)
);
CREATE TABLE IF NOT EXISTS form_mail (
  id BIGSERIAL PRIMARY KEY,
  form_id BIGINT REFERENCES forms(id) ON DELETE CASCADE,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- scroll-thru: a stack of pages/landings/contacts. URL = /{scroll}/{item}
CREATE TABLE IF NOT EXISTS scrolls (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  author_id BIGINT NOT NULL REFERENCES users(id),
  title TEXT NOT NULL,
  slug TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft',
  min_tier INT NOT NULL DEFAULT 0,
  UNIQUE (site_id, slug)
);
CREATE TABLE IF NOT EXISTS scroll_items (
  id BIGSERIAL PRIMARY KEY,
  scroll_id BIGINT NOT NULL REFERENCES scrolls(id) ON DELETE CASCADE,
  piece_id BIGINT REFERENCES pieces(id) ON DELETE CASCADE,
  form_id BIGINT REFERENCES forms(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  pos INT NOT NULL DEFAULT 0
);

-- store
CREATE TABLE IF NOT EXISTS departments (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  descr TEXT NOT NULL DEFAULT '',
  min_tier INT NOT NULL DEFAULT 0,
  UNIQUE (site_id, slug)
);
CREATE TABLE IF NOT EXISTS catalogs (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  min_tier INT NOT NULL DEFAULT 0,
  UNIQUE (site_id, slug)
);
CREATE TABLE IF NOT EXISTS catalog_items (
  catalog_id BIGINT NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE,
  product_id BIGINT REFERENCES products(id) ON DELETE CASCADE,
  department_id BIGINT REFERENCES departments(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS carts (
  id TEXT PRIMARY KEY,
  user_id BIGINT REFERENCES users(id),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS cart_items (
  id BIGSERIAL PRIMARY KEY,
  cart_id TEXT NOT NULL REFERENCES carts(id) ON DELETE CASCADE,
  product_id BIGINT NOT NULL REFERENCES products(id),
  qty INT NOT NULL DEFAULT 1,
  opts JSONB NOT NULL DEFAULT '{}',
  later BOOLEAN NOT NULL DEFAULT FALSE,
  fav BOOLEAN NOT NULL DEFAULT FALSE
);

-- menus (locations are a comma list). Old `menus` table still works as header fallback.
CREATE TABLE IF NOT EXISTS navs (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  places TEXT NOT NULL DEFAULT 'header'
);
CREATE TABLE IF NOT EXISTS nav_items (
  id BIGSERIAL PRIMARY KEY,
  nav_id BIGINT NOT NULL REFERENCES navs(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  href TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'link',
  pos INT NOT NULL DEFAULT 0
);

-- calendar / appointments / reservations
CREATE TABLE IF NOT EXISTS calendars (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  tz TEXT NOT NULL DEFAULT 'America/Detroit',
  slot_min INT NOT NULL DEFAULT 30,
  windows JSONB NOT NULL DEFAULT '{"mon":[["09:00","17:00"]],"tue":[["09:00","17:00"]],"wed":[["09:00","17:00"]],"thu":[["09:00","17:00"]],"fri":[["09:00","17:00"]],"sat":[],"sun":[]}',
  ical_url TEXT,
  caldav_url TEXT,
  google_id TEXT,
  product_id BIGINT REFERENCES products(id) ON DELETE SET NULL,
  min_tier INT NOT NULL DEFAULT 0,
  areas JSONB NOT NULL DEFAULT '{"morning":["06:00","12:00"],"afternoon":["12:00","17:00"],"evening":["17:00","21:00"]}',
  word TEXT NOT NULL DEFAULT 'appointment',
  UNIQUE (site_id, slug)
);
CREATE TABLE IF NOT EXISTS bookings (
  id BIGSERIAL PRIMARY KEY,
  calendar_id BIGINT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
  user_id BIGINT REFERENCES users(id),
  email TEXT,
  starts TIMESTAMPTZ NOT NULL,
  ends TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL DEFAULT 'held',
  pay_via TEXT,
  note TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE sites ADD COLUMN IF NOT EXISTS reader_mode TEXT NOT NULL DEFAULT 'white';

CREATE TABLE IF NOT EXISTS coupons (
  id BIGSERIAL PRIMARY KEY,
  site_id BIGINT REFERENCES sites(id) ON DELETE CASCADE,
  code TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'percent',
  amount INT NOT NULL,
  min_cents INT NOT NULL DEFAULT 0,
  max_uses INT,
  used INT NOT NULL DEFAULT 0,
  expires DATE,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  UNIQUE (site_id, code)
);
ALTER TABLE carts ADD COLUMN IF NOT EXISTS coupon TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS coupon TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS discount_cents INT NOT NULL DEFAULT 0;

ALTER TABLE calendars ADD COLUMN IF NOT EXISTS caldav_user TEXT;
ALTER TABLE calendars ADD COLUMN IF NOT EXISTS caldav_pass TEXT;
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS caldav_uid TEXT;
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS caldav_href TEXT;
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS caldav_etag TEXT;

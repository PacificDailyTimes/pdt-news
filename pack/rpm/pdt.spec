Name:           pdt
Version:        1.0.0
Release:        1%{?dist}
Summary:        pdt-news CMS
License:        GPLv3
URL:            https://github.com/PacificDailyTimes/pdt-news
Source0:        pdt-news-%{version}.tar.gz
BuildRequires:  golang
Requires:       postgresql

%description
News CMS in Go with PostgreSQL, RSS/Atom, shop, and a reader.

%prep
%setup -q -n pdt-news-%{version}

%build
go build -o pdt ./cmd/pdt

%install
mkdir -p %{buildroot}/usr/bin %{buildroot}/usr/share/pdt %{buildroot}/usr/lib/systemd/system
install -m 755 pdt %{buildroot}/usr/bin/pdt
cp -a web sql contrib config.sample %{buildroot}/usr/share/pdt/
install -m 644 contrib/pdt.service %{buildroot}/usr/lib/systemd/system/pdt.service

%files
/usr/bin/pdt
/usr/share/pdt
/usr/lib/systemd/system/pdt.service

%post
echo "Run /usr/share/pdt/contrib/pdt-install (passwordless by default)"

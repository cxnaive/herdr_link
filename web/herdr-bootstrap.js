const e = new URL(window.__HERDR_ENTRY__ || "/builds/0.23.5-491-7b611d0cdeb4f5a5/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);

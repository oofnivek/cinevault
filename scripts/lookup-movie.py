#!/usr/bin/env python3
"""Look up a movie on TMDb and print its correct name and poster URL.

Doesn't touch any files — just tells you what the folder name should be
(`Title (YYYY)`) and gives you a poster URL, so you can rename the folder
and grab the poster yourself.

Usage:
    python3 scripts/lookup-movie.py "some rough title or folder name"

Reads TMDB_API_KEY from the environment, falling back to a .env file in
the repo root (same variable `make fetch-posters` uses). No third-party
dependencies required.
"""

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent


def load_dotenv(path: Path) -> dict:
    values = {}
    if not path.is_file():
        return values
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        values[key.strip()] = value.strip()
    return values


def get_api_key() -> str:
    env = load_dotenv(REPO_ROOT / ".env")
    api_key = os.environ.get("TMDB_API_KEY") or env.get("TMDB_API_KEY")
    if not api_key:
        sys.exit(
            "TMDB_API_KEY is not set — get a v4 Read Access Token at "
            "https://www.themoviedb.org/settings/api and add it to .env"
        )
    return api_key


def tmdb_search(api_key: str, query: str) -> list:
    url = "https://api.themoviedb.org/3/search/movie?" + urllib.parse.urlencode({"query": query})
    req = urllib.request.Request(url, headers={
        "Authorization": f"Bearer {api_key}",
        "Accept": "application/json",
    })
    with urllib.request.urlopen(req) as resp:
        data = json.load(resp)
    return data.get("results", [])


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("title", help="Rough movie title or folder name to search TMDb for")
    args = parser.parse_args()

    api_key = get_api_key()
    query = re.sub(r"\s*\(\d{4}\)\s*$", "", args.title)

    print(f"Searching TMDb for: {query!r}\n")
    try:
        results = tmdb_search(api_key, query)
    except urllib.error.HTTPError as e:
        sys.exit(f"TMDb search failed: {e}")

    if not results:
        print("No results.")
        return

    for r in results:
        year = (r.get("release_date") or "")[:4] or "????"
        poster = r.get("poster_path")
        poster_url = f"https://image.tmdb.org/t/p/w500{poster}" if poster else "(no poster)"
        print(f"{r['title']} ({year})")
        print(f"  folder name : {r['title']} ({year})" if year != "????" else f"  folder name : {r['title']}")
        print(f"  poster      : {poster_url}")
        print(f"  overview    : {(r.get('overview') or '')[:100]}")
        print()


if __name__ == "__main__":
    main()

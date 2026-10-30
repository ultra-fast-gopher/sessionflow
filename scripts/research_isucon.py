#!/usr/bin/env python3
"""Inventory official isucon application Go imports; no credentials required."""
import concurrent.futures
import json
import re
import urllib.request
from datetime import datetime, timezone
from pathlib import Path


def fetch(url):
    with urllib.request.urlopen(url, timeout=30) as response:
        return response.read().decode()


def inventory(repo):
    name, branch = repo['name'], repo['default_branch']
    tree = json.loads(fetch(f'https://api.github.com/repos/isucon/{name}/git/trees/{branch}?recursive=1'))
    paths = [item['path'] for item in tree.get('tree', [])
             if item['type'] == 'blob' and '/vendor/' not in item['path']
             and ('webapp' in item['path'] or 'webapps' in item['path'])
             and (item['path'].endswith('go.mod') or item['path'].endswith('.go'))]
    # Include older GOPATH layouts without scanning vendored dependencies.
    paths = [p for p in paths if (p.endswith('go.mod') or re.search(r'/(go|golang)/.*\.go$', p)) and not any(x in p for x in ('/src/github.com/', '/src/golang.org/', '/src/gopkg.in/', '/pkg/'))]
    found = {}
    for path in paths:
        url = f'https://raw.githubusercontent.com/isucon/{name}/{branch}/{path}'
        body = fetch(url)
        for dep in re.findall(r'(?:github\.com|goji\.io|gopkg\.in)/[\w./-]+', body):
            if any(key in dep for key in ('echo', 'chi/', 'gin-gonic', 'gorilla/mux', 'httprouter', 'goji', 'martini', 'macaron', 'pat/', 'go-json-rest', 'goji.io')):
                found.setdefault(dep, []).append(f'https://github.com/isucon/{name}/blob/{branch}/{path}')
    return {'repository': name, 'branch': branch, 'checked_files': len(paths), 'dependencies': found}


def main():
    repos = json.loads(fetch('https://api.github.com/orgs/isucon/repos?per_page=100'))
    repos = [r for r in repos if re.fullmatch(r'isucon\d+(?:-(?:qualify|final|prior))?', r['name'])]
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        futures = {pool.submit(inventory, repo): repo['name'] for repo in repos}
        results = []
        for future in concurrent.futures.as_completed(futures):
            try:
                result = future.result()
            except Exception as error:
                result = {'repository': futures[future], 'error': str(error)}
            results.append(result)
            print(result['repository'], json.dumps(result.get('dependencies', result.get('error')), ensure_ascii=False), flush=True)
    results.sort(key=lambda row: row['repository'])
    Path('docs/isucon-inventory.json').write_text(json.dumps({'checked_at': datetime.now(timezone.utc).isoformat(timespec='seconds'), 'repositories': results}, indent=2) + '\n')


if __name__ == '__main__':
    main()

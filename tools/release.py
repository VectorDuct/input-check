"""Build a deterministic source ZIP from an explicit publication allowlist."""
import argparse
import hashlib
from pathlib import Path
import zipfile

ROOT = Path(__file__).resolve().parents[1]
FILES = ['README.md', 'README.zh-CN.md', 'LICENSE', 'PROVENANCE.md', 'CONTRIBUTING.md',
         'CHANGELOG.md', 'SECURITY.md', '.editorconfig', 'go.mod', 'Makefile', 'Dockerfile', 'Dockerfile.prebuilt',
         'compose.yaml', '.gitignore', '.dockerignore']
PATTERNS = ['cmd/inputcheck/*.go', 'internal/check/*.go', 'internal/codec/*.go',
            'internal/gateway/*.go', 'examples/*.json', 'docs/*.md',
            '.github/workflows/*.yml', 'tools/*.py']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=ROOT / 'dist/input-check-0.1.0.zip')
    args = parser.parse_args()
    files = {ROOT / name for name in FILES}
    for pattern in PATTERNS:
        files.update(ROOT.glob(pattern))
    payloads = {}
    for path in sorted(files, key=lambda item: item.relative_to(ROOT).as_posix()):
        if not path.is_file() or path.is_symlink() or not path.resolve().is_relative_to(ROOT):
            raise ValueError('Missing/unsafe file in publication allowlist')
        payloads[path.relative_to(ROOT).as_posix()] = path.read_bytes()
    manifest = ''.join(hashlib.sha256(data).hexdigest() + '  ' + name + '\n' for name, data in payloads.items())
    payloads['MANIFEST.sha256'] = manifest.encode()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(args.output, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
        for name, data in payloads.items():
            info = zipfile.ZipInfo('input-check/' + name, date_time=(2026, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            archive.writestr(info, data)
    print(f'{args.output.name}: {len(payloads)} files; sha256={hashlib.sha256(args.output.read_bytes()).hexdigest()}')


if __name__ == '__main__':
    main()

#!/usr/bin/env python3
"""Gera as fixtures de testdata/sources a partir das fontes reais.

As fixtures são recortes pequenos e consistentes dos arquivos publicados:
os cabeçalhos e as linhas summary dos arquivos delegated são recalculados
para bater com os registros escolhidos, e os blocos do NIC.br só citam
prefixos presentes no recorte da LACNIC (como acontece nos dados reais).

Uso (com as fontes baixadas em um diretório):

    python3 testdata/make-fixtures.py /caminho/das/fontes testdata/sources

Arquivos esperados no diretório de origem: afrinic.txt apnic.txt arin.txt
lacnic.txt ripencc.txt nicbr.txt asn.txt rdap-asn.json rdap-ipv4.json
rdap-ipv6.json e os CSVs da IANA com o nome original.
"""

import hashlib
import ipaddress
import os
import shutil
import sys

SRC, DST = sys.argv[1], sys.argv[2]
os.makedirs(DST, exist_ok=True)

# ASNs de interesse: brasileiros (TMSoft, RNP), estrangeiros que aparecem no
# NIC.br (Cogent, Microsoft, Tatu do Bem) e alguns de cada RIR.
NICBR_ASNS = {61613, 1916, 174, 8075, 2635}
EXTRA_ASNS = {
    "arin": {3356, 15169},
    "ripencc": {3333, 1136},
    "apnic": {4608, 13335},
    "afrinic": {37100, 327700},
    "lacnic": {28573},
}
EXTRA_V4 = {
    "arin": {"8.0.0.0", "8.8.0.0", "8.8.4.0", "8.8.8.0"},
    "apnic": {"1.0.0.0", "1.1.1.0"},
    "ripencc": {"193.0.0.0"},
    "afrinic": {"41.0.0.0"},
}
EXTRA_V6 = {
    "arin": {"2001:4860::"},
    "apnic": {"2001:200::"},
    "ripencc": {"2001:67c:2e8::"},
}


def records(path):
    header, out = None, []
    for line in open(path, encoding="utf-8"):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        p = line.split("|")
        if header is None:
            header = p
            continue
        if len(p) >= 6 and p[5] == "summary":
            continue
        out.append(p)
    return header, out


def cidrs(rec):
    if rec[2] == "ipv4":
        start = ipaddress.IPv4Address(rec[3])
        return list(ipaddress.summarize_address_range(start, start + int(rec[4]) - 1))
    if rec[2] == "ipv6":
        return [ipaddress.ip_network(f"{rec[3]}/{rec[4]}")]
    return []


def write_delegated(rir, header, recs):
    counts = {"asn": 0, "ipv4": 0, "ipv6": 0}
    for r in recs:
        counts[r[2]] += 1
    header = list(header)
    header[3] = str(len(recs))
    lines = ["|".join(header)]
    for typ in ("asn", "ipv4", "ipv6"):
        lines.append(f"{rir}|*|{typ}|*|{counts[typ]}|summary")
    lines += ["|".join(r) for r in recs]
    name = f"rir-{rir}"
    data = ("\n".join(lines) + "\n").encode()
    open(os.path.join(DST, name), "wb").write(data)
    md5 = hashlib.md5(data).hexdigest()
    open(os.path.join(DST, name + ".md5"), "w").write(f"MD5 ({name}) = {md5}\n")


# NIC.br: linhas dos ASNs escolhidos.
nicbr_lines = {}
for line in open(os.path.join(SRC, "nicbr.txt"), encoding="utf-8"):
    p = line.strip().split("|")
    if p and p[0].startswith("AS") and int(p[0][2:]) in NICBR_ASNS:
        nicbr_lines[int(p[0][2:])] = p
nicbr_prefixes = set()
for p in nicbr_lines.values():
    for x in p[3:]:
        if x:
            nicbr_prefixes.add(ipaddress.ip_network(x))

for rir in ("afrinic", "apnic", "arin", "lacnic", "ripencc"):
    header, recs = records(os.path.join(SRC, f"{rir}.txt"))
    chosen, holders = [], set()
    nonaligned = available = reserved = 0
    for r in recs:
        take = False
        if r[2] == "asn":
            first, count = int(r[3]), int(r[4])
            wanted = NICBR_ASNS | EXTRA_ASNS.get(rir, set())
            take = any(first <= a < first + count for a in wanted)
        elif r[2] == "ipv4":
            take = r[3] in EXTRA_V4.get(rir, set())
        elif r[2] == "ipv6":
            take = r[3] in EXTRA_V6.get(rir, set())
        if not take and rir == "lacnic" and r[2] in ("ipv4", "ipv6"):
            take = any(c in nicbr_prefixes for c in cidrs(r))
        # Registros IPv4 fora de CIDR existem na ARIN, no RIPE e na AFRINIC.
        if not take and r[2] == "ipv4" and r[6] == "allocated" and nonaligned < 1 and len(cidrs(r)) > 1:
            take = True
            nonaligned += 1
        if not take and r[6] == "available" and available < 2:
            take = True
            available += 1
        if not take and r[6] == "reserved" and reserved < 2:
            take = True
            reserved += 1
        if take:
            chosen.append(r)
    write_delegated(rir, header, chosen)

# NIC.br: só blocos que existem no recorte da LACNIC (nos dados reais, 100%).
_, lacnic_recs = records(os.path.join(DST, "rir-lacnic"))
lacnic_cidrs = set()
for r in lacnic_recs:
    lacnic_cidrs.update(cidrs(r))
with open(os.path.join(DST, "nicbr"), "w", encoding="utf-8") as f:
    for asn in sorted(nicbr_lines):
        p = nicbr_lines[asn]
        pfx = [x for x in p[3:] if x and ipaddress.ip_network(x) in lacnic_cidrs]
        f.write("|".join(p[:3] + pfx) + "\n")

# asn.txt: nomes de todos os ASNs presentes nos recortes.
fixture_asns = set()
for rir in ("afrinic", "apnic", "arin", "lacnic", "ripencc"):
    _, recs = records(os.path.join(DST, f"rir-{rir}"))
    for r in recs:
        if r[2] == "asn":
            fixture_asns.update(range(int(r[3]), int(r[3]) + int(r[4])))
with open(os.path.join(DST, "asnames"), "w", encoding="utf-8") as f:
    for line in open(os.path.join(SRC, "asn.txt"), encoding="utf-8"):
        num = line.split(" ", 1)[0]
        if num.isdigit() and int(num) in fixture_asns:
            f.write(line)

# IANA e bootstrap RDAP: arquivos inteiros (são pequenos).
copies = {
    "iana-asn-16": "as-numbers-1.csv",
    "iana-asn-32": "as-numbers-2.csv",
    "iana-ipv4": "ipv4-address-space.csv",
    "iana-ipv6": "ipv6-unicast-address-assignments.csv",
    "iana-special-ipv4": "iana-ipv4-special-registry-1.csv",
    "iana-special-ipv6": "iana-ipv6-special-registry-1.csv",
    "iana-special-asn": "special-purpose-as-numbers.csv",
    "iana-rdap-asn": "rdap-asn.json",
    "iana-rdap-ipv4": "rdap-ipv4.json",
    "iana-rdap-ipv6": "rdap-ipv6.json",
}
for dst, src in copies.items():
    shutil.copyfile(os.path.join(SRC, src), os.path.join(DST, dst))

print("fixtures geradas em", DST)

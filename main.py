def get_lines(file):
    lines = []
    with open(file, encoding="utf8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            if lines and not lines[-1].endswith(" ."):
                lines[-1] += " " + line
            else:
                lines.append(line)
    return lines


def get_maps(lines):

    maps = dict()

    for line in lines:
        key, val = line.split(" = ")
        key = key.strip()
        val = val.strip()
        maps[key] = val
    return maps
    # return dict(sorted(maps.items(), key=lambda x: x[0][0].isupper()))


def sort_maps(maps, token):
    tokens = dict((k, list(t for t in token(v) if t in maps)) for k, v in maps.items())
    sent = set()

    def send(k, space=0):
        if k not in sent:
            yield space, k, maps.pop(k)
            sent.add(k)
            for k in tokens[k]:
                yield from send(k, space + 4)

    yield from send("SourceFile")


if __name__ == "__main__":
    import re, sys

    rf, wf, *_ = sys.argv[1:] * 2
    maps = get_maps(get_lines(rf))
    with open(wf, encoding="utf8", mode="w") as f:
        for s, k, v in sort_maps(maps, lambda v: re.findall("\\w+", v)):
            f.write(f"{' ' *s}{k} = {v}\n")

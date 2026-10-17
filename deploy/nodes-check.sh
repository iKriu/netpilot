#!/bin/sh
# Проверка узлов через Clash API ядра на сервере: что выбрала группа «Авто»,
# и задержка нескольких узлов.
sudo python3 - <<'EOF'
import json, urllib.request, urllib.parse, concurrent.futures
conf = json.load(open("/var/lib/netpilot/sing-box.json"))
api = "http://" + conf["experimental"]["clash_api"]["external_controller"]
names = {("node-" + n["id"]): n["name"] for n in json.load(open("/var/lib/netpilot/config.json"))["nodes"]}

def get(path):
    return json.load(urllib.request.urlopen(api + path, timeout=15))

auto = get("/proxies/auto")
print("группа Авто сейчас:", names.get(auto.get("now"), auto.get("now")))
proxy = get("/proxies/proxy")
print("proxy сейчас:", proxy.get("now"))

def delay(tag):
    try:
        r = get("/proxies/%s/delay?timeout=5000&url=%s" % (urllib.parse.quote(tag), urllib.parse.quote("https://www.gstatic.com/generate_204")))
        return tag, r.get("delay", -1)
    except Exception as e:
        return tag, -1

tags = list(names)[:60]
ok = 0
with concurrent.futures.ThreadPoolExecutor(16) as ex:
    for tag, d in ex.map(delay, tags):
        if d > 0:
            ok += 1
            if ok <= 8:
                print("  работает: %4d мс  %s" % (d, names[tag]))
print("из первых %d узлов работают: %d" % (len(tags), ok))
EOF

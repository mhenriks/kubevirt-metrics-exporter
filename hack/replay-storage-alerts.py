#!/usr/bin/env python3
"""Replay captured OpenMetrics through real Prometheus rule evaluation.

No customer data or identifiers are written into the repository. Generates
promtool fixtures and JSON results under --output. Reference is the frozen
pre-aggregation manifest; candidate is the deployed manifest. Sparse exported
range-query samples are represented by explicit staleness on disappearance.
No PVC/VMI metadata is fabricated. Requires PyYAML and promtool.
"""
import argparse
import array
import datetime
from decimal import Decimal
import gzip
import json
import math
from pathlib import Path
import re
import subprocess
import yaml

ROOT=Path(__file__).resolve().parents[1]
PREFIXES=('kubevirt_vmi_storage_io_latency_seconds_', 'kme_block_io_latency_seconds_',
          'kme_nfs_io_latency_seconds_', 'kubevirt_vmi_storage_guest_latency_avg_seconds',
          'kubevirt_vmi_info', 'kube_persistentvolumeclaim_info')


def load(files,start,n,timestamp_unit="seconds"):
    divisor = Decimal(1000 if timestamp_unit == "milliseconds" else 1)
    data={}
    for path in files:
        opener = gzip.open if str(path).endswith('.gz') else open
        with opener(path,'rt') as f:
            for line in f:
                if not line.startswith(PREFIXES):continue
                key,value,ts=line.rsplit(None,2)
                elapsed = Decimal(ts) / divisor - start
                if elapsed < 0: continue
                i = int(elapsed // 30)
                if 0<=i<n:
                    if key not in data:data[key]=array.array('d',[math.nan])*n
                    data[key][i]=float(value)
    return data


def compact(a):
    # Explicit stale markers prevent vanished series lingering for lookback_delta.
    result=[]; previous=None; run=0; was_present=False
    def emit(v,n):
        if v in ('_','stale'):return ' '.join([v]*n)
        return v if n==1 else f'{v}+0x{n-1}'
    for x in a:
        present=math.isfinite(x)
        token=format(x,'.17g') if present else ('stale' if was_present else '_')
        was_present=present
        if token==previous:run+=1
        else:
            if run:result.append(emit(previous,run))
            previous=token;run=1
    if run:result.append(emit(previous,run))
    return ' '.join(result)


def inputs(data):
    active=set()
    for key,a in data.items():
        metric=key.split('{')[0]
        if metric.endswith('_count'):
            vals=[v for v in a if math.isfinite(v)]
            if vals and min(vals)!=max(vals):active.add(key.replace('_count{','{'))
    out=[]
    for key,a in data.items():
        if not key.startswith(PREFIXES):continue
        metric=key.split('{')[0]
        if '_latency_seconds_' in metric:
            if metric.endswith('_sum'):continue
            base=key.replace('_count{','{').replace('_bucket{','{')
            base=re.sub(r',le="[^"]*"','',base)
            base=re.sub(r'le="[^"]*",','',base)
            if base not in active:continue
        if not any(math.isfinite(v) for v in a):continue
        out.append(dict(series=key,values=compact(a)))
    return out


# Quote-aware patterns: braces and escaped quotes inside label values are data.
LABEL = re.compile(r'(\w+)=("(?:\\.|[^"\\])*")')
VECTOR_ITEM = re.compile(r'\{((?:[^"{}]|"(?:\\.|[^"\\])*")*)\}\s+([-+0-9.eE]+|NaN|[+-]?Inf)')


def parse_vector(value):
    alerts = []
    position = 0
    for match in VECTOR_ITEM.finditer(value):
        if value[position:match.start()].strip(' ,'):
            raise ValueError('Unexpected vector content')
        labels = {}
        cursor = 0
        for label in LABEL.finditer(match[1]):
            if match[1][cursor:label.start()].strip(' ,'):
                raise ValueError('Unexpected label content')
            # Prometheus uses Go quoting; translate hexadecimal escapes to JSON.
            quoted = re.sub(r'\\x([0-9a-fA-F]{2})', r'\\u00\1', label[2])
            labels[label[1]] = json.loads(quoted)
            cursor = label.end()
        if match[1][cursor:].strip(' ,') or labels.get('__name__') != 'ALERTS' or not labels.get('alertname'):
            raise ValueError('Incomplete alert labels')
        if float(match[2]) != 1:
            raise ValueError('Unexpected ALERTS value')
        alerts.append(labels)
        position = match.end()
    if not alerts or value[position:].strip(' ,'):
        raise ValueError('Unrecognized alert vector')
    return alerts


def parse_results(text,start):
    """Accept only expected-empty expression mismatches, rejecting every other error."""
    timeline = {}
    current = None
    expected_empty = False
    for line in text.splitlines():
        line = line.strip()
        if not line or line in ('FAILED:', 'SUCCESS'):
            continue
        match = re.fullmatch(r'expr: "ALERTS.*", time: ([0-9.hms]+),', line)
        if match:
            if current is not None:
                raise ValueError('Incomplete promtool result')
            seconds = sum(float(v)*{'h':3600,'m':60,'s':1}[u] for v,u in re.findall(r'([\d.]+)([hms])',match[1]))
            current = int(start + seconds)
            expected_empty = False
        elif line == 'exp: nil' and current is not None:
            expected_empty = True
        elif line.startswith('got: ') and current is not None and expected_empty:
            if current in timeline:
                raise ValueError('Duplicate evaluation time')
            timeline[current] = parse_vector(line[5:])
            current = None
        else:
            raise ValueError('Unexpected promtool output: ' + line[:200])
    if current is not None:
        raise ValueError('Incomplete promtool result')
    return timeline


def summarize(timeline,n,start):
    identities={};by_alert={};peak=0
    for ts,alerts in timeline.items():
        peak=max(peak,len(alerts))
        for lab in alerts:
            key=json.dumps(lab,sort_keys=True)
            identities.setdefault(key,[]).append(ts)
            by_alert.setdefault(lab['alertname'],set()).add(key)
    episodes=[]
    for key,times in identities.items():
        times.sort();begin=prev=times[0]
        for ts in times[1:]+[None]:
            if ts is None or ts-prev>30:
                episodes.append(dict(labels=json.loads(key),start_utc=datetime.datetime.fromtimestamp(begin,datetime.timezone.utc).isoformat(),last_firing_utc=datetime.datetime.fromtimestamp(prev,datetime.timezone.utc).isoformat(),observed_firing_seconds=prev-begin+30))
                begin=ts
            prev=ts
    return dict(unique_instances=len(identities),peak_concurrent=peak,instances_by_alert={k:len(v) for k,v in by_alert.items()},episodes=episodes)


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--capture',action='append',default=[],help='timestamped text or .gz capture; repeat for multiple files')
    p.add_argument('--start',type=int,required=True,help='UTC epoch seconds of first sample')
    p.add_argument('--hours',type=int,required=True)
    p.add_argument('--output',type=Path,required=True)
    p.add_argument('--promtool',default='promtool')
    p.add_argument('--timestamp-unit',choices=['seconds','milliseconds'],default='seconds',
                   help='seconds for OpenMetrics (default); milliseconds for legacy range exports')
    args=p.parse_args();out=args.output;out.mkdir(parents=True,exist_ok=True);n=args.hours*120+1
    if args.hours <= 0:p.error('--hours must be positive')
    if not args.capture:p.error('provide at least one --capture')
    data=load(args.capture,args.start,n,args.timestamp_unit)
    inp=inputs(data)
    if not inp:p.error('no usable samples in the requested window; check captures, start and timestamp unit')
    print('Replay input series:',len(inp),flush=True)
    manifests={'before':ROOT/'test/promtool/fixtures/storage-latency-before-aggregation.yaml','after':ROOT/'deploy/prometheus-rules/prometheusrule.yaml'}
    results={}
    for name,path in manifests.items():
        rules=yaml.safe_load(path.read_text())['spec']['groups']
        # Explicit cadence for a comparable replay of the pre-aggregation rules.
        for group in rules:group.setdefault('interval','30s')
        rulepath=out/(name+'-rules.yml');rulepath.write_text(yaml.safe_dump(dict(groups=rules),sort_keys=False))
        expr='ALERTS{alertstate="firing",alertname=~".*Latency.*"}'
        tests=dict(rule_files=[str(rulepath.resolve())],evaluation_interval='30s',group_eval_order=[g['name'] for g in rules],tests=[dict(name=name,interval='30s',input_series=inp,promql_expr_test=[dict(expr=expr,eval_time=f'{i*30}s',exp_samples=[]) for i in range(n)])])
        testpath=out/(name+'-replay.yml');testpath.write_text(yaml.safe_dump(tests,sort_keys=False,width=1000000))
        # Nonempty actual vectors intentionally differ from [] so promtool prints
        # exact evaluated alert instances. Parse only expected expression mismatches.
        log=out/(name+'-actual.txt')
        with log.open('w') as f:ret=subprocess.run([args.promtool,'test','rules',str(testpath)],stdout=f,stderr=subprocess.STDOUT)
        text=log.read_text()
        if ret.returncode not in (0,1):
            raise RuntimeError('Replay failed; inspect '+str(log))
        try:
            timeline=parse_results(text,args.start)
        except ValueError as exc:
            raise RuntimeError('Replay failed; inspect '+str(log)) from exc
        if bool(ret.returncode) != bool(timeline):
            raise RuntimeError('Unexpected promtool exit status; inspect '+str(log))
        result=summarize(timeline,n,args.start);results[name]=result
        (out/(name+'-timeline.json')).write_text(json.dumps(timeline))
        print(name,{k:v for k,v in result.items() if k!='episodes'},flush=True)
    results['limitations']=['No fabricated metadata: absent PVC/storage-class and VMI info remain absent.', 'Missing exported samples become stale at the 30s grid boundary.', 'No pre-window history: initial pending timers start within the capture.', 'Replay counts firing alert instances, not delivered notifications.']
    (out/'comparison.json').write_text(json.dumps(results,indent=2))

if __name__=='__main__':main()

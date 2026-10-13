#!/usr/bin/env python3
"""Exercise aggregation with real histogram inputs and isolated transition tests."""
import copy
from pathlib import Path
import yaml

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent
DOC = yaml.safe_load((ROOT / 'deploy/prometheus-rules/prometheusrule.yaml').read_text())
GROUPS = DOC['spec']['groups']
ALERTS = {r['alert']: r for g in GROUPS for r in g['rules'] if 'alert' in r}


def series(metric, labels, values='1+0x60'):
    return {'series': metric + '{' + ','.join(f'{k}="{v}"' for k, v in labels.items()) + '}', 'values': values}


def labels(name='vm', disk='root', claim='pvc', signal='qmp', pod='launcher'):
    d = dict(namespace='ns', node='node-a', pod=pod, operation='write', persistentvolumeclaim=claim, signal=signal)
    if name:
        d.update(name=name, disk=disk)
    return d


def expected(alert, identity):
    r = ALERTS[alert]
    scope = {'VMIStorageLatencyHigh': 'vmi', 'PodStorageLatencyHigh': 'pod', 'StorageClassLatencyHigh': 'storageclass'}[alert]
    out = dict(severity='warning', kubernetes_operator_part_of='kubevirt',
               kubernetes_operator_component='kubevirt' if scope == 'vmi' else 'kubevirt-metrics-exporter',
               operator_health_impact='none', incident='storage-latency', scope=scope)
    if scope == 'storageclass': out['namespace'] = 'kubevirt-metrics-exporter'
    out.update(identity)
    annotations = {}
    for k, v in r['annotations'].items():
        if k in ('affected','active'): v = 'Counts unavailable during recovery grace.'
        if k == 'latency': v = 'Latency samples unavailable.'
        for label, val in out.items():
            v = v.replace('{{ $labels.' + label + ' }}', val)
        annotations[k] = v
    return dict(exp_labels=out, exp_annotations=annotations)


def check(t, vmi=(), pods=(), shared=()):
    return [dict(eval_time=t, alertname=a, exp_alerts=[expected(a, x) for x in ids]) for a, ids in [
        ('VMIStorageLatencyHigh', vmi), ('PodStorageLatencyHigh', pods), ('StorageClassLatencyHigh', shared)]]

VMI = dict(namespace='ns', name='vm', operation='write')
POD = dict(namespace='ns', pod='launcher', operation='write')
SC = dict(storageclass='sc', operation='write')
INFO = series('kubevirt_vmi_info', dict(namespace='ns', name='vm', vmi_pod='launcher', phase='running'))
PVC = series('kube_persistentvolumeclaim_info', dict(namespace='ns', persistentvolumeclaim='pvc', storageclass='sc'))


def condition(d, values='1+0x60', duration=10):
    return series(f'kme:storage_latency_condition_{duration}m:bool', d, values)


def member(d):
    return series('kme:storage_latency_summary_member:bool', d)


def shared(values='1+0x60', signal='qmp'):
    return series('storageclass:kme_latency_condition:bool', dict(storageclass='sc', operation='write', signal=signal), values)


def main():
    (OUT / 'aggregation-rules.yml').write_text(yaml.safe_dump({'groups': GROUPS}, sort_keys=False))
    # Feed exact boolean condition transitions to the production maturity and
    # suppression records. Raw-expression coverage is exercised separately below.
    stages = copy.deepcopy(GROUPS)
    excluded = {'kme:storage_latency_condition_10m:bool', 'kme:storage_latency_condition_15m:bool',
                'storageclass:kme_latency_condition:bool', 'kme:storage_latency_summary_member:bool'}
    for g in stages:
        g['rules'] = [r for r in g['rules'] if r.get('record') not in excluded]
    (OUT / 'aggregation-stages.yml').write_text(yaml.safe_dump({'groups': stages}, sort_keys=False))
    tests = []
    def case(name, inputs, checks):
        tests.append(dict(name=name, interval='30s', input_series=inputs, alert_rule_test=checks))
    d = labels()
    case('one VMI across disks and sources', [condition(d), condition(labels(disk='data')), condition(labels(name='', signal='block')), INFO], check('9m30s') + check('10m', [VMI]))
    case('disk handoff cannot satisfy persistence', [condition(d, '1+0x11 stale _x48'), condition(labels(disk='data'), '_x11 1+0x48')], check('15m') + check('16m', [VMI]))
    case('source handoff cannot satisfy persistence', [condition(d, '1+0x11 stale _x48'), condition(labels(name='', signal='block'), '_x11 1+0x48'), INFO], check('15m') + check('16m', [VMI]))
    case('evaluated condition disappearance resets continuity', [condition(d, '1+0x11 stale 1+0x47')], check('15m') + check('16m30s', [VMI]))
    case('guest retains fifteen minute persistence', [condition(labels(signal='guest'), duration=15)], check('14m30s') + check('15m', [VMI]))
    case('matching shared source covers workload', [condition(d), member(d), shared(), PVC], check('10m', shared=[SC]))
    case('different source summary cannot suppress', [condition(d), member(d), shared(signal='block'), PVC], check('10m', [VMI], shared=[SC]))
    case('below summary activity floor remains visible', [condition(d), shared(), PVC], check('10m', [VMI], shared=[SC]))
    case('unmapped PVC remains visible', [condition(d), member(d), shared()], check('10m', [VMI], shared=[SC]))
    case('second uncovered disk preserves workload', [condition(d), member(d), condition(labels(disk='other',claim='other')), shared(), PVC], check('10m', [VMI], shared=[SC]))
    b = labels(name='', signal='block')
    case('backup excluded from VMI summary suppression', [condition(b), member(b), shared(signal='block'), PVC], check('10m', pods=[POD], shared=[SC]))
    case('identified VMI block signal covered', [condition(b), member(b), shared(signal='block'), PVC, INFO], check('10m', shared=[SC]))
    case('summary recovery exposes already mature workload immediately', [condition(d), member(d), shared('1+0x23 stale _x36'), PVC], check('11m', shared=[SC]) + check('12m', shared=[SC]) + check('13m30s', [VMI]))
    case('PVC metadata loss restores qualified workload', [condition(d), member(d), shared(), dict(PVC, values='1+0x23 stale _ _ _ _ _ 1+0x30')], check('11m', shared=[SC]) + check('12m', shared=[SC]) + check('13m30s', [VMI], shared=[SC]) + check('15m', shared=[SC]))
    case('brief VMI metadata gap preserves aggregation', [condition(b), dict(INFO, values='1+0x23 stale _x2 1+0x32')], check('12m', [VMI]) + check('14m', [VMI]))
    case('expired VMI metadata preserves pod coverage', [condition(b), dict(INFO, values='1+0x3 stale _x56')], check('10m', pods=[POD]))
    case('QMP absent block still detects', [condition(b), INFO], check('10m', [VMI]))
    case('separate namespaces do not merge', [condition(d), condition(dict(d,namespace='other'))], check('10m', [VMI, dict(VMI, namespace='other')]))
    case('read and write stay separate', [condition(d), condition(dict(d,operation='read'))], check('10m', [VMI, dict(VMI,operation='read')]))
    case('multiple summary sources generate one shared alert', [shared(), shared(signal='block')], check('10m', shared=[SC]))
    case('missing condition sample retains qualification', [condition(d,'1+0x23 _ 1+0x50')], check('11m',[VMI])+check('12m30s',[VMI]))
    case('prolonged missing condition resets qualification', [condition(d,'1+0x23 _x5 1+0x50')], check('13m30s')+check('14m30s')+check('24m30s',[VMI]))
    case('brief shared condition dip retains coverage', [condition(d), member(d), shared('1+0x23 stale 1+0x50'), PVC], check('12m',shared=[SC])+check('12m30s',shared=[SC])+check('22m',shared=[SC]))
    case('bounded initial wait for pending shared summary', [condition(d), member(d), shared('_ _ 1+0x60'), PVC], check('10m')+check('11m',shared=[SC]))
    case('pending summary cannot delay workload indefinitely', [condition(d), member(d), shared('_x9 1+0x60'), PVC], check('10m')+check('11m30s',[VMI])+check('15m',shared=[SC]))
    case('pending summary recovery releases workload immediately', [condition(d), member(d), shared('_ _ _ _ _ _ _ _ _ _ 1+0x10 stale _x40'), PVC], check('10m')+check('10m30s',[VMI]))
    case('PVC remap prefers current metadata over cached mapping', [condition(d), member(d), shared(), dict(PVC,values='1+0x23 stale _x36'), series('kube_persistentvolumeclaim_info',dict(namespace='ns',persistentvolumeclaim='pvc',storageclass='new'), '_x23 1+0x36')], check('11m',shared=[SC])+check('12m',[VMI],shared=[SC]))
    case('repeated isolated missing samples do not prevent qualification', [condition(d,'1+0x7 _ 1+0x7 _ 1+0x7 _ 1+0x30')], check('10m',[VMI])+check('14m',[VMI]))
    case('evaluated recovery resets already mature workload', [condition(d,'1+0x23 stale 1+0x50')], check('11m',[VMI])+check('12m')+check('22m')+check('22m30s',[VMI]))
    case('long shared dip expires grace and requires requalification', [condition(d), member(d), shared('1+0x23 stale _ _ _ _ _ 1+0x60'), PVC], check('12m',shared=[SC])+check('13m30s',[VMI])+check('15m',[VMI])+check('24m30s',[VMI])+check('25m',shared=[SC]))
    case('source handoff cannot retain shared qualification', [shared('1+0x23 stale _x40'), shared('_ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ 1+0x50',signal='block')], check('12m',shared=[SC])+check('13m30s')+check('22m',shared=[SC]))
    case('long PVC metadata loss keeps workload visible', [condition(d), member(d), shared(), dict(PVC,values='1+0x23 stale _x60')], check('12m',shared=[SC])+check('13m30s',[VMI],shared=[SC])+check('25m',[VMI],shared=[SC]))
    case('ambiguous cached PVC mappings cannot suppress', [condition(d), member(d), shared(), dict(PVC,values='1+0x23 stale _x60'), series('kube_persistentvolumeclaim_info',dict(namespace='ns',persistentvolumeclaim='pvc',storageclass='other'),'1+0x23 stale _x60')], check('12m',[VMI],shared=[SC]))
    case('PVC samples without stale markers still expire', [condition(d), member(d), shared(), dict(PVC,values='1+0x23 _x60')], check('12m',shared=[SC])+check('13m30s',[VMI],shared=[SC]))
    (OUT / 'aggregation-stage-tests.yml').write_text(yaml.safe_dump(dict(rule_files=['aggregation-stages.yml'], evaluation_interval='30s', tests=tests), sort_keys=False))
    raw = []
    def hist(kind, name, disk, pvc, slow=True, step=60):
        lab = dict(namespace='ns',name=name,node='node-a',pod='launcher-'+name,disk=disk,persistentvolumeclaim=pvc,operation='write')
        metric='kubevirt_vmi_storage_io_latency_seconds'
        if kind=='block':
            metric='kme_block_io_latency_seconds';lab.pop('name');lab.pop('disk')
        out=[series(metric+'_count',lab,f'0+{step}x60')]
        out += [series(metric+'_bucket',dict(lab,le=le),f'0+{val}x60') for le,val in [('0.01',0),('0.1',int(step*.8) if slow else step),('1.0',step),('+Inf',step)]]
        return out
    raw.append(dict(name='raw QMP disks consolidate without PVC metadata', interval='30s', input_series=hist('qmp','vm','a','')+hist('qmp','vm','b',''), alert_rule_test=check('10m')+check('10m30s',[VMI])))
    inputs=[]
    for i in range(3):
        name=f'vm{i}';pvc=f'pvc{i}'
        inputs+=hist('qmp',name,'root',pvc)+hist('block',name,'root',pvc)
        inputs += [series('kube_persistentvolumeclaim_info',dict(namespace='ns',persistentvolumeclaim=pvc,storageclass='sc')),series('kubevirt_vmi_info',dict(namespace='ns',name=name,vmi_pod='launcher-'+name,phase='running'))]
    raw.append(dict(name='raw independent QMP and block summaries consolidate', interval='30s',input_series=inputs,alert_rule_test=check('10m30s') + check('11m',shared=[SC]) + check('15m',shared=[SC])))
    raw.append(dict(name='raw healthy QMP remains quiet', interval='30s', input_series=hist('qmp','vm','root','pvc',slow=False), alert_rule_test=check('25m')))
    raw.append(dict(name='raw block backup needs no VMI or PVC metadata', interval='30s', input_series=hist('block','vm','root','pvc'), alert_rule_test=check('10m') + check('10m30s', pods=[dict(POD,pod='launcher-vm')])) )
    raw.append(dict(name='raw guest preserves threshold and persistence', interval='30s', input_series=[series('kubevirt_vmi_storage_guest_latency_avg_seconds', dict(namespace='ns',name='vm',node='node-a',disk='root',operation='write'), '0.2+0x60')], alert_rule_test=check('14m30s') + check('15m',[VMI])))
    flush=[]
    for i in range(3):
        name=f'vm{i}';pvc=f'pvc{i}'
        flush += [dict(x,series=x['series'].replace('operation="write"','operation="flush"')) for x in hist('qmp',name,'root',pvc,step=1)]
        flush += [series('kube_persistentvolumeclaim_info',dict(namespace='ns',persistentvolumeclaim=pvc,storageclass='sc'))]
    raw.append(dict(name='raw low rate flush keeps individual coverage below summary floor', interval='30s', input_series=flush, alert_rule_test=check('15m') + check('15m30s',[dict(VMI,name=f'vm{i}',operation='flush') for i in range(3)])))
    # Expected diagnostics are independent of the production annotation templates.
    for test in raw:
        for assertion in test['alert_rule_test']:
            for expected_alert in assertion['exp_alerts']:
                annotations = expected_alert['exp_annotations']
                if assertion['alertname'] == 'StorageClassLatencyHigh':
                    annotations.update(affected='block: 3; qmp: 3; ', active='block: 3; qmp: 3; ')
                else:
                    text = 'qmp: 955ms; '
                    if 'backup' in test['name']: text = 'block: 955ms; '
                    if 'guest' in test['name']: text = 'guest: 200ms; '
                    if 'flush' in test['name']: text = 'qmp: 991ms; '
                    annotations['latency'] = text
    (OUT / 'aggregation-raw-tests.yml').write_text(yaml.safe_dump(dict(rule_files=['aggregation-rules.yml'],evaluation_interval='30s',tests=raw),sort_keys=False))
    print(f'Generated {len(tests)} transition tests and {len(raw)} raw-metric tests')

if __name__ == '__main__':
    main()

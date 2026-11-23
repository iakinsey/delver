#!/usr/bin/env python3
"""Run compiled Go benchmarks locally against synthetic fixtures.

No source checkout, production service, proxy, or production data is changed.
Raw results remain outside the repository. Each case includes an unchanged
baseline control and five alternating baseline/candidate pairs.
"""
import argparse
import hashlib
import json
import pathlib
import subprocess
import uuid
import shutil
import sys

MEASURE = r'''
import json,os,pathlib,subprocess,time,statistics,hashlib,resource
c=json.loads(CONFIG)
root=pathlib.Path(c['root']);data=pathlib.Path(c.get('storage_root') or root/'data');data.mkdir(parents=True,exist_ok=True)
env=dict(os.environ,GOMAXPROCS=str(c['procs']),TMPDIR=str(data))
def observation():
 out={'time':time.time(),'loadavg':pathlib.Path('/proc/loadavg').read_text().strip()}
 for name,path in [('frequency_khz',f'/sys/devices/system/cpu/cpu{c["cpu"]}/cpufreq/scaling_cur_freq'),('governor',f'/sys/devices/system/cpu/cpu{c["cpu"]}/cpufreq/scaling_governor')]:
  try:out[name]=pathlib.Path(path).read_text().strip()
  except OSError:pass
 return out
rows=[]
for repeat,variant in [(-2,'baseline'),(-1,'baseline')]+[(i,v) for i in range(5) for v in (['baseline','candidate'] if i%2==0 else ['candidate','baseline'])]:
 before=observation();started=time.monotonic();usage_before=resource.getrusage(resource.RUSAGE_CHILDREN)
 args=['nice','-n','10','taskset','-c',str(c['cpu']),str(root/variant),'-test.run','^$','-test.bench',c['benchmark'],'-test.benchtime',c['benchtime'],'-test.benchmem']
 label=f'{variant}-{repeat+1}';log_path=root/(label+'.log');log=log_path.open('w')
 process=subprocess.Popen(args,env=env,stdout=log,stderr=subprocess.STDOUT,text=True)
 peak_rss=peak_fds=write_bytes=0
 while process.poll() is None:
  try:
   status=pathlib.Path(f'/proc/{process.pid}/status').read_text()
   for line in status.splitlines():
    if line.startswith('VmHWM:'):peak_rss=max(peak_rss,int(line.split()[1]))
   peak_fds=max(peak_fds,len(list(pathlib.Path(f'/proc/{process.pid}/fd').iterdir())))
   for line in pathlib.Path(f'/proc/{process.pid}/io').read_text().splitlines():
    if line.startswith('write_bytes:'):write_bytes=max(write_bytes,int(line.split()[1]))
  except OSError:pass
  if time.monotonic()-started>c['timeout']:
   process.kill();process.wait();log.close();raise RuntimeError('benchmark timeout')
  time.sleep(.02)
 process.wait();log.close();output=log_path.read_text();usage_after=resource.getrusage(resource.RUSAGE_CHILDREN)
 if process.returncode:raise RuntimeError(label+': '+output)
 measurements=[];pending_case=None
 for line in output.splitlines():
  fields=line.split()
  if not fields:continue
  if fields[0].startswith('Benchmark'):
   pending_case=fields[0]
  elif pending_case and fields[0].isdigit():
   fields=[pending_case]+fields
  else:continue
  if len(fields)<4 or not fields[1].isdigit():continue
  try:metrics={fields[i+1]:float(fields[i]) for i in range(2,len(fields)-1,2)}
  except ValueError:raise RuntimeError('malformed benchmark result: '+line)
  if 'ns/op' not in metrics:continue
  name=fields[0];suffix='-'+str(c['procs'])
  if c['procs']>1 and name.endswith(suffix):name=name[:-len(suffix)]
  measurements.append({'case':name,'iterations':int(fields[1]),**metrics});pending_case=None
 if not measurements:raise RuntimeError('no benchmark cases executed')
 row={'repeat':repeat,'variant':variant,'before':before,'after':observation(),'measurements':measurements,'including_setup':{'cpu_seconds':usage_after.ru_utime+usage_after.ru_stime-usage_before.ru_utime-usage_before.ru_stime,'peak_rss_kib':peak_rss,'peak_fds':peak_fds,'write_bytes':write_bytes}}
 rows.append(row);(root/'raw.json').write_text(json.dumps(rows,indent=2));print(json.dumps({'repeat':repeat,'variant':variant,'cases':len(measurements)}),flush=True)
names={m['case'] for r in rows for m in r['measurements']};summary=[]
for name in sorted(names):
 variants={}
 for variant in ['baseline','candidate']:
  samples=[m for r in rows if r['variant']==variant and r['repeat']>=0 for m in r['measurements'] if m['case']==name];assert len(samples)==5,name
  variants[variant]={'median':{k:statistics.median(m[k] for m in samples) for k in samples[0] if k not in ['case','iterations']},'cv':statistics.stdev(m['ns/op'] for m in samples)/statistics.mean(m['ns/op'] for m in samples)}
 control=[m['ns/op'] for r in rows if r['repeat']<0 for m in r['measurements'] if m['case']==name];assert len(control)==2
 direction=sum(next(m['ns/op'] for r in rows if r['repeat']==i and r['variant']=='candidate' for m in r['measurements'] if m['case']==name)<next(m['ns/op'] for r in rows if r['repeat']==i and r['variant']=='baseline' for m in r['measurements'] if m['case']==name) for i in range(5))
 summary.append({'case':name,'variants':variants,'control_drift':abs(control[1]/control[0]-1),'candidate_faster_pairs':direction})
result={'configuration':c,'binary_sha256':{v:hashlib.sha256((root/v).read_bytes()).hexdigest() for v in ['baseline','candidate']},'samples':rows,'summary':summary}
(root/'results.json').write_text(json.dumps(result,indent=2));print(json.dumps({'finished':True,'root':str(root)}),flush=True)
'''


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--baseline', type=pathlib.Path, required=True)
    p.add_argument('--candidate', type=pathlib.Path, required=True)
    p.add_argument('--benchmark', required=True)
    p.add_argument('--benchtime', default='50x')
    p.add_argument('--cpu', type=int, default=0)
    p.add_argument('--procs', type=int, default=1)
    p.add_argument('--timeout', type=int, default=600)
    p.add_argument('--output', type=pathlib.Path, required=True)
    p.add_argument('--storage-root', type=pathlib.Path, help='optional local synthetic data directory, e.g. /dev/shm')
    a = p.parse_args()
    a.output.mkdir(parents=True, exist_ok=True)
    token = uuid.uuid4().hex
    root=str(pathlib.Path.home()/'.cache/delver-efficiency'/token)
    pathlib.Path(root).mkdir(parents=True,mode=0o700)
    for variant in ['baseline','candidate']:
        shutil.copy2(getattr(a,variant).resolve(strict=True),pathlib.Path(root)/variant)
    config = {'storage_root':str(a.storage_root.resolve()/token) if a.storage_root else None,'root':root,'benchmark':a.benchmark,'benchtime':a.benchtime,'cpu':a.cpu,'procs':a.procs,'timeout':a.timeout}
    (a.output/'configuration.json').write_text(json.dumps(config,indent=2))
    code = MEASURE.replace('CONFIG',repr(json.dumps(config)),1)
    result = subprocess.run([sys.executable,'-'],input=code,text=True)
    for name in ['raw.json','results.json']:
        if (pathlib.Path(root)/name).exists(): shutil.copy2(pathlib.Path(root)/name,a.output/name)
    if result.returncode:
        raise SystemExit(result.returncode)
    report=json.loads((a.output/'results.json').read_text())
    inconclusive=any(max(x['variants'][v]['cv'] for v in ['baseline','candidate'])>.1 or x['control_drift']>.1 for x in report['summary'])
    (a.output/'disposition.json').write_text(json.dumps({'inconclusive':inconclusive,'automatic_acceptance':False,'requires_resource_and_correctness_review':True},indent=2))
    print(json.dumps({'output':str(a.output),'inconclusive':inconclusive}))
    if inconclusive:
        raise SystemExit(2)


if __name__ == '__main__':
    main()

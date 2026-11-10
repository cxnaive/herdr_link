// Exercise the published upstream UI renderers AND input handlers, without a model or real tools.
import assert from 'node:assert/strict';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';

const repo = resolve(import.meta.dirname, '..');
const pins = JSON.parse(await readFile(join(repo, 'tests/fixtures/pi-omp-upstream.json'), 'utf8'));
assert(process.env.PI_OMP_RUNTIME && process.env.PI_OMP_WORK && process.env.PI_OMP_GO_TEST, 'Set PI_OMP_RUNTIME, PI_OMP_WORK and PI_OMP_GO_TEST');
const runtime = resolve(process.env.PI_OMP_RUNTIME);
const work = resolve(process.env.PI_OMP_WORK);
assert.equal(process.env.HOME, join(work, 'home'), 'Run with the isolated fixture HOME');
assert.equal(process.env.PI_CODING_AGENT_DIR, join(work, 'home/.pi/agent'));
await mkdir(join(work, 'frames'), { recursive: true });
await mkdir(join(work, 'home/.pi/agent'), { recursive: true });
const pkg = name => join(runtime, 'node_modules', name);
const load = path => import(pathToFileURL(path));
for (const [provider, pin] of Object.entries(pins)) {
  if (typeof pin !== 'object') continue;
  const manifest = JSON.parse(await readFile(join(pkg(pin.package), 'package.json'), 'utf8'));
  assert.equal(manifest.name, pin.package); assert.equal(manifest.version, pin.version, `${provider}: re-audit changed versions`);
}
const pi = pkg(pins.pi.package);
const omp = pkg('@oh-my-pi/pi-tui');
assert.equal(JSON.parse(await readFile(join(omp, 'package.json'), 'utf8')).version, pins.omp.version);
const { ExtensionSelectorComponent: PiSelect } = await load(join(pi, 'dist/modes/interactive/components/extension-selector.js'));
const { ExtensionInputComponent: PiInput } = await load(join(pi, 'dist/modes/interactive/components/extension-input.js'));
const { initTheme } = await load(join(pi, 'dist/modes/interactive/theme/theme.js'));
const { KeybindingsManager: PiKeys } = await load(join(pi, 'dist/core/keybindings.js'));
const { setKeybindings: setPiKeys } = await load(join(pkg('@earendil-works/pi-tui'), 'dist/index.js'));
const { ApprovalDialogComponent: Approval } = await load(join(omp, 'src/overlays/approval-dialog.ts'));
const { AskDialogComponent: Ask } = await load(join(omp, 'src/overlays/ask-dialog.ts'));
const { HookEditorComponent: HookEditor } = await load(join(omp, 'src/overlays/hook-editor.ts'));
const { PlanReviewOverlay: Plan } = await load(join(omp, 'src/overlays/plan-review-overlay.ts'));
const { HtmlFormDialog: Form } = await load(join(omp, 'src/html-form/dialog.ts'));
const { parseHtmlForm } = await load(join(omp, 'src/html-form/parse.ts'));
const { getThemeByName, setThemeInstance } = await load(join(omp, 'src/theme/index.ts'));
const { KeybindingsManager: OmpKeys } = await load(join(omp, 'src/app-keybindings.ts'));
const { setKeybindings: setOmpKeys } = await load(join(omp, 'src/index.ts'));
const key = { Up: '\x1b[A', Down: '\x1b[B', Left: '\x1b[D', Right: '\x1b[C', Enter: '\r', Space: ' ', Tab: '\t', 'Shift+Tab': '\x1b[Z', 'Ctrl+E': '\x05', 'Ctrl+U': '\x15', End: '\x1b[F', n: 'n', Esc: '\x1b', Escape: '\x1b' };
const render = (c, width) => c.render(width).join('\n');
const sleep = ms => new Promise(r => setTimeout(r, ms));
let serial = 0;
async function classify(agent, frame, intent = {}, approvalIndex) {
  const input = join(work, `request-${++serial}.json`), output = join(work, `result-${serial}.json`);
  await writeFile(input, JSON.stringify({ Agent: agent, Frame: frame, Intent: intent, ApprovalIndex: approvalIndex }));
  const run = spawnSync(process.env.PI_OMP_GO_TEST, ['-test.run=^TestPiOMPUpstreamContract$', '-test.timeout=10s'], {
    cwd: repo, encoding: 'utf8', timeout: 15000,
    env: { PATH: process.env.PATH, HOME: process.env.HOME, HERDR_INTERACTION_INPUT: input, HERDR_INTERACTION_OUTPUT: output },
  });
  assert.equal(run.status, 0, run.stdout + run.stderr);
  return JSON.parse(await readFile(output, 'utf8'));
}
function typeText(c,text,width){c.handleInput('\x1b[200~'+text+'\x1b[201~');c.render(width);}
function feed(c, keys, width) {
  for (const k of keys) { assert(k in key, `Unmapped key ${k}`); c.handleInput(key[k]); c.render(width); }
}
function askWithEditor(questions, done, cancel) {
  let editor;
  const tui={terminal:{rows:40},requestRender(){}};
  const ask=new Ask(questions,{onSubmit:done,onCancel:cancel,onPrompt:(prompt,prefill)=>new Promise(resolve=>{
    editor=new HookEditor(tui,prompt.title,prefill,text=>{editor=undefined;resolve(text)},()=>{editor=undefined;resolve(undefined)},{promptStyle:true,question:prompt.question});
    editor.focused=true;
  })});
  return {render:width=>(editor??ask).render(width),handleInput:data=>(editor??ask).handleInput(data),dispose(){editor?.dispose();ask.dispose();}};
}
function form(html,done,cancel){const c=new Form(parseHtmlForm(html),{onSubmit:done,onCancel:cancel});c.focused=true;return c;}
function harness(factory) {
  const events = [];
  const done = value => events.push({ type: 'submit', value });
  const cancel = () => events.push({ type: 'cancel' });
  const prompts = [];
  const component = factory(done, cancel, prompts);
  return { component, events, prompts };
}
const choices = ['Approve and execute', 'Approve and compact context', 'Approve and keep context', 'Refine plan'];
const scenarios = [
  { name: 'pi-confirm', agent: 'pi', make: (done, cancel) => new PiSelect('Allow synthetic action?\nNo tool will execute.', ['Yes', 'No'], done, cancel), nativeKeys: ['Down', 'Enter'], expected: 'No', intent: { Selected: [1] } },
  { name: 'pi-select', agent: 'pi', make: (done, cancel) => new PiSelect('Choose a region', ['North', 'South'], done, cancel), nativeKeys: ['Down', 'Enter'], expected: 'South', intent: { Selected: [1] } },
  { name: 'pi-input', agent: 'pi', make: (done, cancel) => new PiInput('Describe your choice', 'details', done, cancel), nativeText: 'synthetic answer', nativeKeys: ['Enter'], expected: 'synthetic answer', intent: { OtherSelected: true, OtherText: 'synthetic answer' } },
  { name: 'omp-approval', agent: 'omp', make: (done, cancel) => new Approval('Allow tool: bash\nCommand: echo synthetic', { onApprove: () => done('approve'), onDeny: cancel }), nativeKeys: ['Enter'], expected: 'approve', intent: {Selected:[0]}, arm: true },
  { name: 'omp-session-approval', agent: 'omp', make: (done, cancel) => new Approval('Allow tool: bash\nCommand: echo synthetic', { onApprove: () => done('approve'), onDeny: cancel }, { session: { label: 'Approve for this session', onApprove: () => done('session') } }), nativeKeys: ['Down', 'Enter'], expected: 'session', intent: {Selected:[1]}, arm: true },
  { name: 'omp-grant-approval', agent: 'omp', make: (done, cancel) => new Approval('Allow tool: write\nPath: /synthetic/file', { onApprove: () => done('approve'), onDeny: cancel }, { grants: [{ label: 'Allow this folder', alwaysLabel: 'Always allow this folder' }], onGrant: (_g, always) => done(always ? 'always' : 'session') }), nativeKeys: ['Enter'], expected: 'approve', intent: {Selected:[2]}, arm: true },
  ...choices.map((label, index) => ({ name: `omp-plan-${index}`, agent: 'omp', make: (done, cancel) => new Plan('# Synthetic plan\n\nDescribe changes only.', { promptTitle: 'Plan mode - next step', options: choices, helpText: 'esc cancel' }, { onPick: done, onCancel: cancel }), nativeKeys: [...Array(index).fill('Down'), 'Enter'], expected: label, approval: index })),
  { name: 'omp-ask-single', agent: 'omp', make: (done, cancel, prompts) => new Ask([{ id: 'region', question: 'Choose a region', options: [{ label: 'North' }, { label: 'South' }] }], { onSubmit: done, onCancel: cancel, onPrompt: p => { prompts.push(p); return Promise.resolve(undefined); } }), nativeKeys: ['Down', 'Enter'], check: events => assert.equal(events[0]?.value?.results?.[0]?.selectedOptions?.[0], 'South'), intent: { Selected: [1] } },
  { name: 'omp-ask-multi', agent: 'omp', make: (done, cancel, prompts) => new Ask([{ id: 'features', question: 'Select features', multi: true, options: [{ label: 'Logging' }, { label: 'Metrics' }, { label: 'Tracing' }] }], { onSubmit: done, onCancel: cancel, onPrompt: p => { prompts.push(p); return Promise.resolve(undefined); } }), nativeKeys: ['Space', 'Down', 'Down', 'Space', 'Enter', 'Enter'], check: events => assert.deepEqual(events[0]?.value?.results?.[0]?.selectedOptions, ['Logging', 'Tracing']), intent: { Selected: [0, 2] } },
  { name: 'omp-html-form', agent: 'omp', make: (done, cancel) => { const c = new Form(parseHtmlForm('<form><h2>Deployment settings</h2><fieldset><legend>Region</legend><label><input type="radio" name="region" value="north" checked>North</label><label><input type="radio" name="region" value="south">South</label></fieldset><label>Notes<input name="notes" value="synthetic"></label><button type="submit">Submit</button></form>'), { onSubmit: done, onCancel: cancel }); c.focused = true; return c; }, nativeKeys: ['Down', 'Space', 'Tab', 'Tab', 'Enter'], check: events => { assert.equal(events[0]?.value?.region, 'south'); assert.equal(events[0]?.value?.notes, 'synthetic'); }, intent: { Selected: [2] }, followups: [{label:'选择当前项并到下一字段'},{label:'下一字段（不提交）'},{label:'提交整个表单'}] },
];
scenarios.push(
  {name:'pi-empty-input',agent:'pi',make:(done,cancel)=>new PiInput('Optional note','',done,cancel),nativeKeys:['Enter'],expected:'',intent:{OtherSelected:true,OtherText:''}},
  {name:'omp-approval-deny',agent:'omp',make:(done,cancel)=>new Approval('Allow tool: bash\nCommand: echo synthetic',{onApprove:()=>done('approve'),onDeny:()=>done('deny')}),nativeKeys:['Down','Enter'],expected:'deny',intent:{Selected:[1]},arm:true,cancelResult:{type:'submit',value:'deny'}},
  {name:'omp-remember-grant',agent:'omp',make:(done,cancel)=>new Approval('Allow tool: write\nPath: /synthetic/file',{onApprove:()=>done('approve'),onDeny:cancel},{grants:[{label:'Allow this folder',alwaysLabel:'Always allow this folder'}],onGrant:(_g,always)=>done(always?'always':'session')}),nativeKeys:['Up','Up','Enter','Down','Enter'],expected:'always',intent:{Selected:[0]},followups:[{label:'Always allow this folder'}],arm:true},
  {name:'omp-multi-empty',agent:'omp',make:(done,cancel)=>new Ask([{id:'features',question:'Select features',multi:true,options:[{label:'A'},{label:'B'}]}],{onSubmit:done,onCancel:cancel,onPrompt:()=>Promise.resolve(undefined)}),nativeKeys:['Enter'],check:e=>assert.deepEqual(e[0]?.value?.results?.[0]?.selectedOptions,[]),intent:{Selected:[]}},
  {name:'omp-ask-two',agent:'omp',make:(done,cancel)=>new Ask([{id:'first',question:'First?',options:[{label:'A'},{label:'B'}]},{id:'second',question:'Second?',options:[{label:'C'},{label:'D'}]}],{onSubmit:done,onCancel:cancel,onPrompt:()=>Promise.resolve(undefined)}),nativeKeys:['Down','Enter','Down','Enter','Enter'],check:e=>assert.deepEqual(e[0]?.value?.results?.map(r=>r.selectedOptions),[['B'],['D']]),intent:{Selected:[1]},followups:[{label:'D'},{label:'提交以上答案'}]},
  {name:'omp-ask-custom',agent:'omp',make:(done,cancel)=>askWithEditor([{id:'region',question:'Choose a region',options:[{label:'North'},{label:'South'}]}],done,cancel),nativeSequence:[{keys:['Down','Down','Enter']},{text:'自定义地区'},{keys:['Enter']}],check:e=>assert.equal(e[0]?.value?.results?.[0]?.customInput,'自定义地区'),intent:{Selected:[2]},followups:[{text:'自定义地区'},{label:'提交当前答案'}]},
  {name:'omp-ask-note',agent:'omp',make:(done,cancel)=>askWithEditor([{id:'region',question:'Choose a region',options:[{label:'North'},{label:'South'}]}],done,cancel),nativeSequence:[{keys:['n']},{text:'备注'},{keys:['Enter']},{keys:['Enter']}],check:e=>{assert.deepEqual(e[0]?.value?.results?.[0]?.selectedOptions,['North']);assert.equal(e[0]?.value?.results?.[0]?.note,'备注')},intent:{Selected:[3]},followups:[{text:'备注'},{label:'提交当前答案'},{label:'North  ✎ note'}]},
  {name:'omp-form-checkbox',agent:'omp',make:(done,cancel)=>form('<form><fieldset><legend>Features</legend><label><input type="checkbox" name="features" value="a">A</label><label><input type="checkbox" name="features" value="b">B</label></fieldset></form>',done,cancel),nativeKeys:['Space','Down','Space','Tab','Enter'],check:e=>assert.deepEqual(e[0]?.value?.features,['a','b']),intent:{Selected:[0]},followups:[{label:'向下移动'},{label:'切换当前复选项'},{label:'下一字段（不提交）'},{label:'提交整个表单'}]},
  {name:'omp-form-text',agent:'omp',make:(done,cancel)=>form('<form><label>Notes<input name="notes"></label></form>',done,cancel),nativeSequence:[{text:'中文答案'},{keys:['Tab','Enter']}],check:e=>assert.equal(e[0]?.value?.notes,'中文答案'),intent:{OtherSelected:true,OtherText:'中文答案'},followups:[{label:'下一字段（不提交）'},{label:'提交整个表单'}]},
  {name:'omp-form-textarea',agent:'omp',make:(done,cancel)=>form('<form><label>Notes<textarea name="notes"></textarea></label></form>',done,cancel),nativeSequence:[{text:'line one\n第二行'},{keys:['Tab','Enter']}],check:e=>assert.equal(e[0]?.value?.notes,'line one\n第二行'),intent:{OtherSelected:true,OtherText:'line one\n第二行'},followups:[{label:'下一字段（不提交）'},{label:'提交整个表单'}]},
  {name:'omp-form-dropdown',agent:'omp',make:(done,cancel)=>form('<form><label>Database<select name="db"><option value="a">Alpha</option><option value="b">Beta</option></select></label></form>',done,cancel),nativeKeys:['Right','Tab','Enter'],check:e=>assert.equal(e[0]?.value?.db,'b'),intent:{Selected:[2]},followups:[{label:'下一字段（不提交）'},{label:'提交整个表单'}]}
);
const results = [];

for (const theme of ['dark', 'light']) {
  initTheme(theme); setPiKeys(new PiKeys());
  setThemeInstance(await getThemeByName(theme)); setOmpKeys(OmpKeys.inMemory());
  for (const width of [80, 120]) for (const s of scenarios) {
    const h = harness(s.make); h.component.render(width);
    const frame = render(h.component, width);
    const id = `${s.name}-${theme}-${width}`;
    await writeFile(join(work, 'frames', id + '.ansi'), frame);
    const translated = await classify(s.agent, frame, s.intent, s.approval);
    if (s.arm) await sleep(510); // Same anti-accidental-input delay as the upstream component.
    const check = events => { assert.equal(events.length, 1, `${id}: exact single callback expected`); if (s.check) s.check(events); else assert.deepEqual(events[0], { type: 'submit', value: s.expected }); };
    const supported = translated.classification.Kind === (s.approval === undefined ? 'question' : 'approval');
    let verdict = 'unsupported';
    if (supported) {
      assert.equal(translated.validation_error,'',`${id}: ${translated.validation_error}`);
      for (const step of translated.steps ?? []) {
        feed(h.component, step.Keys ?? [], width);
        if (step.Text) { typeText(h.component,step.Text,width); }
      }
      await sleep(0);
      for(const followup of s.followups ?? []) {
        assert.equal(h.events.length,0,`${id}: no premature final submit`);
        let next=await classify(s.agent,render(h.component,width));
        const index=next.classification.Interaction?.options?.find(o=>o.label===followup.label)?.index;
        if(followup.text===undefined) assert.notEqual(index,undefined,`${id}: missing followup ${followup.label}: ${JSON.stringify(next.classification)}`);
        next=await classify(s.agent,render(h.component,width),followup.text!==undefined?{OtherSelected:true,OtherText:followup.text}:{Selected:[index]});
        assert.equal(next.validation_error,'',`${id}: ${next.validation_error}`);
        for(const step of next.steps??[]) {feed(h.component,step.Keys??[],width);if(step.Text)typeText(h.component,step.Text,width);}
        await sleep(0);
      }
      try { check(h.events); verdict = 'supported'; } catch { verdict = 'wrong-result'; }
    } else assert(!translated.steps?.length, `${id}: unsupported UI must not synthesize input`);
    h.component.dispose?.();
    const reference = harness(s.make); reference.component.render(width);
    if (s.arm) await sleep(510);
    if (s.nativeText) typeText(reference.component,s.nativeText,width);
    feed(reference.component, s.nativeKeys??[], width); await sleep(0);
    for(const step of s.nativeSequence??[]) { if(step.text!==undefined)typeText(reference.component,step.text,width); feed(reference.component,step.keys??[],width); await sleep(0); }
    try { check(reference.events); } catch (error) { console.error(id, 'native callback:', JSON.stringify(reference.events)); throw error; }
    reference.component.dispose?.();
    const cancellation = harness(s.make); cancellation.component.render(width);
    feed(cancellation.component, ['Escape'], width); await sleep(0);
    assert.deepEqual(cancellation.events, [s.cancelResult??{ type: 'cancel' }], `${id}: cancellation must not approve or submit`);
    cancellation.component.dispose?.();
    results.push({ case: s.name, theme, width, verdict, relay_kind: translated.classification.Kind, steps: translated.steps, native_result: reference.events });
    console.log(`${verdict.toUpperCase()} ${id}`);
  }
}
const report = { versions: pins, scope: 'Published upstream render/input components + actual Relay parser/planner; no LLM, tools, personal data, live Herdr or device automation', results };
await writeFile(join(work, 'report.json'), JSON.stringify(report, null, 2) + '\n');
const count = verdict => results.filter(r => r.verdict === verdict).length;
console.log(JSON.stringify({ supported: count('supported'), unsupported: count('unsupported'), wrong_result: count('wrong-result'), report: join(work, 'report.json') }));
assert.equal(count('wrong-result'), 0, 'Recognized controls delivered the wrong upstream answer');
if (process.env.PI_OMP_REQUIRE_ALL !== '0') assert.equal(count('unsupported'), 0, 'Current upstream support is incomplete; see report.json');

#!/usr/bin/env node
/**
 * Apifox 接口批量导入工具（Node 版，用原生 fetch —— 与 apifox-import-mcp 同栈）。
 *
 * 用法：
 *   node scripts/apifox-import.mjs <enhanced_dir>            # 全量导入
 *   node scripts/apifox-import.mjs <enhanced_dir> --start 5  # 从第 5 个 op 开始（断点续传）
 *   node scripts/apifox-import.mjs <enhanced_dir> --dry-run  # 演练
 *
 * 日志：tmp/apifox-import.log（每次成功/失败带时间戳）
 */

import fs from 'fs';
import path from 'path';

// ---------- 配置 ----------
const APIFOX_TOKEN = 'afxp_c65976cn2QVJmnyMLA1N2tQL2VG2nueqQZ7W';
const PROJECT_ID = '6542782';
const API_BASE = 'https://api.apifox.com';
const API_VERSION = '2024-03-28';
const LOG_FILE = 'tmp/apifox-import.log';
const DELAY_MS = 300;

// ---------- 日志 ----------
function log(msg, level = 'INFO') {
  const ts = new Date().toISOString().replace('T', ' ').substring(0, 19);
  const line = `[${ts}] [${level}] ${msg}`;
  console.log(line);
  fs.appendFileSync(LOG_FILE, line + '\n');
}

// ---------- Apifox API ----------
async function apifoxImport(openapiDoc) {
  const url = `${API_BASE}/v1/projects/${PROJECT_ID}/import-openapi`;
  const body = JSON.stringify({
    input: JSON.stringify(openapiDoc),  // 关键：字符串不是对象
    options: { endpointOverwriteBehavior: 'AUTO_MERGE', updateFolderOfChangedEndpoint: true },
  });
  const resp = await fetch(url, {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${APIFOX_TOKEN}`,
      'X-Apifox-Api-Version': API_VERSION,
      'Content-Type': 'application/json',
    },
    body,
  });
  const text = await resp.text();
  if (resp.status !== 200) {
    throw new Error(`HTTP ${resp.status}: ${text.substring(0, 300)}`);
  }
  return JSON.parse(text);
}

// ---------- 单文件导入 ----------
async function importFile(filepath, kind, dryRun) {
  let d;
  try { d = JSON.parse(fs.readFileSync(filepath, 'utf-8')); }
  catch (e) { log(`✗ [${kind}] ${path.basename(filepath)} JSON 解析失败: ${e.message}`, 'ERROR'); return false; }

  const name = d.info?.title || '';
  const nP = Object.keys(d.paths || {}).length;
  const nS = Object.keys(d.components?.schemas || {}).length;
  const label = `[${kind}] ${path.basename(filepath)} paths=${nP} schemas=${nS}`;

  if (dryRun) { log(`○ ${label} → dry-run 跳过`); return true; }

  try {
    const result = await apifoxImport(d);
    const c = result.data?.counters || {};
    log(`✓ ${label} → ep:+${c.endpointCreated||0} ~${c.endpointUpdated||0}, schema:+${c.schemaCreated||0} ~${c.schemaUpdated||0} (ignored:${c.schemaIgnored||0})`);
    return true;
  } catch (e) {
    log(`✗ ${label} → ${e.message}`, 'ERROR');
    return false;
  }
}

// ---------- 主流程 ----------
async function main() {
  const args = process.argv.slice(2);
  const srcDir = args[0] || 'tmp/apifox-enhanced';
  const dryRun = args.includes('--dry-run');
  let startIdx = 0;
  const startFlag = args.indexOf('--start');
  if (startFlag !== -1 && args[startFlag + 1]) startIdx = parseInt(args[startFlag + 1]) - 1;

  log('========== Apifox 批量导入开始 ==========');
  log(`项目: ${PROJECT_ID} | 源目录: ${srcDir} | dry_run: ${dryRun} | start: ${startIdx + 1}`);

  const allFiles = fs.readdirSync(srcDir).filter(f => f.endsWith('.json')).sort();
  const schemaFiles = allFiles.filter(f => f.startsWith('s-')).map(f => path.join(srcDir, f));
  const opFiles = allFiles.filter(f => f.startsWith('o-')).map(f => path.join(srcDir, f));
  log(`schema: ${schemaFiles.length} | operation: ${opFiles.length}`);

  let okCount = 0, failCount = 0;
  const failedFiles = [];

  // Phase 1: schemas
  if (startIdx === 0) {
    log(`---- Phase 1: Schemas (${schemaFiles.length}) ----`);
    for (const f of schemaFiles) {
      const ok = await importFile(f, 'schema', dryRun);
      if (ok) okCount++; else { failCount++; failedFiles.push(f); }
      await new Promise(r => setTimeout(r, DELAY_MS));
    }
  }

  // Phase 2: operations
  const todoOps = startIdx > 0 ? opFiles.slice(startIdx - 1) : opFiles;
  log(`---- Phase 2: Operations (${todoOps.length}) ----`);
  for (let i = 0; i < todoOps.length; i++) {
    const ok = await importFile(todoOps[i], 'op', dryRun);
    if (ok) okCount++; else { failCount++; failedFiles.push(todoOps[i]); }
    if ((i + 1) % 20 === 0) log(`  进度: ${i + 1}/${todoOps.length} (成功 ${okCount} / 失败 ${failCount})`);
    await new Promise(r => setTimeout(r, DELAY_MS));
  }

  log('========== 导入完成 ==========');
  log(`总计: ${okCount + failCount} | 成功: ${okCount} | 失败: ${failCount}`);
  if (failedFiles.length > 0) {
    log(`失败文件（${failedFiles.length} 个）：`, 'WARN');
    failedFiles.forEach(f => log(`  ${path.basename(f)}`, 'WARN'));
    log(`断点续传: node scripts/apifox-import.mjs ${srcDir} --start <编号>`, 'WARN');
  }
}

// path/fileURLToPath imports must be at top of file
// (moved from bottom: ESM requires top-level imports)

main().catch(e => { log(`FATAL: ${e.message}`, 'ERROR'); process.exit(1); });

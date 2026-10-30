const button = document.getElementById('check');
const output = document.getElementById('result');
const labels = {not_required: '当前无需验证（不是已通过）', pending: '验证处理中', approved: '接口返回已通过', denied: '接口返回未通过', unknown: '未知状态，需查看官方页面'};
const errors = {http_401: '登录失效，请登录后重试', http_403: '访问被拒，不等于 KYC 未通过', http_404: '接口不存在或已变化', http_429: '请求过频，停止查询', wrong_origin: '请切换到 https://claude.ai 标签页', timeout: '请求超时', network_or_redirect: '网络失败或发生重定向', invalid_json: '响应不是 JSON', invalid_organizations: '组织数据格式变化', no_organizations: '未返回组织', too_many_organizations: '组织超过 10 个，未继续批量查询', invalid_organization: '组织标识无效', time_budget: '达到本次查询时间上限'};
button.onclick = async () => {
  button.disabled = true;
  output.textContent = '正在查询，请保持弹窗打开…';
  try {
    const [tab] = await chrome.tabs.query({active: true, currentWindow: true});
    if (!tab?.url || new URL(tab.url).origin !== 'https://claude.ai') {
      output.textContent = errors.wrong_origin;
      return;
    }
    const results = await chrome.scripting.executeScript({target: {tabId: tab.id}, func: checkKyc});
    const data = results[0]?.result;
    if (!data) throw new Error('empty');
    if (data.error) { output.textContent = errors[data.error] || '未取得状态'; return; }
    output.textContent = data.rows.map(row => '组织 ' + row.organization + '：' +
      (row.error ? errors[row.error] || '查询失败' : labels[row.status] || labels.unknown)).join('\n') +
      '\n查询时间：' + data.checkedAt + (data.incomplete ? '\n部分查询未完成，未继续请求。' : '');
  } catch (_) { output.textContent = '查询未完成，请核对扩展权限及当前 Claude 标签页。'; }
  finally { button.disabled = false; }
};

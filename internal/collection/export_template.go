package collection

// 本文件承载 ExportHTML 的文档骨架：单文件交付要求样式与脚本内联，
// 但这些内容与导出逻辑无关，留在函数体里会把 ExportHTML 撑到 400+ 行无法维护，
// 故拆成包级常量，由 ExportHTML 按顺序拼接。
//
// 修改样式/脚本时只动这里；注意它们出现在 <script> 内的字符串里不能含反引号
// （Go raw string 的定界符），必要时改用字符串拼接。

// htmlDocHead 文档头：DOCTYPE 到 <title> 开标签（标题文本由调用方转义后写入）。
const htmlDocHead = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>`

// htmlStyleBlock 内联样式表：从 </title> 到 <body> 结束，含 TOC / 正文 / 大纲的排版与响应式规则。
const htmlStyleBlock = `</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body { font: 14px/1.7 -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Noto Sans SC", sans-serif;
         margin: 0; padding: 0; color: #202124; background: #fff; }
  /* 正文容器默认占视口的 90%（左右各留 5%），不再固定 1100px —— 宽屏下文档铺开更好读，
     过长的行由各区块自己控制（表格/代码块横向溢出滚动，不撑破页面）。 */
  .layout { display: flex; gap: 24px; max-width: 90%; margin: 0 auto; padding: 32px 24px; align-items: flex-start; }
  /* 左侧 sticky 目录树（集合树）：窄栏、浅底卡片，滚动时固定；
     标题与搜索框钉在栏顶部，只有下方树区内部滚动（overflow 从 nav 下沉到 .toc-list）。 */
  .toc { flex: 0 0 240px; position: sticky; top: 16px; max-height: calc(100vh - 32px); overflow: hidden;
         display: flex; flex-direction: column;
         border: 1px solid #e3e3e3; border-radius: 10px; background: #f5f6f8; padding: 12px 14px; font-size: 13px; }
  .toc-title { font-weight: 600; color: #202124; margin-bottom: 8px; flex: 0 0 auto; }
  /* 目录搜索框（固定在树上方，不随列表滚动） */
  .toc-search { width: 100%; box-sizing: border-box; margin: 0 0 10px; padding: 6px 8px; flex: 0 0 auto;
    border: 1px solid #e3e3e3; border-radius: 6px; font-size: 13px; font-family: inherit;
    color: #202124; background: #fff; outline: none; }
  .toc-search:focus { border-color: #18a058; }
  .toc-list, .toc ul { list-style: none; margin: 0; padding: 0; }
  /* 树区占满剩余高度并内部滚动（min-height:0 是 flex 子项收缩的前提）。 */
  .toc-list { flex: 1 1 auto; min-height: 0; overflow: auto; }
  /* 嵌套目录：保留左侧缩进，并加树形连接线（风格对齐 client 侧栏树） */
  .toc ul { padding-left: 14px; }
  .toc li { margin: 2px 0; }
  /* 连接线：每个嵌套 li 画一段竖线（拼接成连续竖线），再用 ::after 画短横线连到文字；
     末项的竖线只到横线（中线），形成 └ 形，不向下多余延伸 */
  .toc-list ul > li { position: relative; }
  .toc-list ul > li::before {
    content: ""; position: absolute; top: -3px; bottom: 0; left: -8px;
    border-left: 1px solid #e3e3e3;
  }
  .toc-list ul > li::after {
    content: ""; position: absolute; top: 11px; left: -8px; width: 8px;
    border-top: 1px solid #e3e3e3;
  }
  .toc-list ul > li:last-child::before { bottom: auto; height: 14px; }
  /* 折叠/展开：文件夹默认展开（aria-expanded=true）才显示子列表；折叠时隐藏 */
  .toc-folder[aria-expanded="false"] > ul { display: none; }
  /* 文件夹开关：右侧三角形，展开时旋转 90° 指向下方；
     用 -1px 微调使其与文字视觉居中（三角形几何中心略低于文本中线） */
  .toc-toggle { display: inline-block; width: 0; height: 0; margin: 0 6px 0 2px;
    border-style: solid; border-width: 4px 0 4px 6px; border-color: transparent transparent transparent #6b7280;
    vertical-align: middle; position: relative; top: -1px; cursor: pointer; transition: transform .15s ease; }
  .toc-folder[aria-expanded="true"] > .toc-toggle { transform: rotate(90deg); }
  .toc a { color: #202124; text-decoration: none; display: block; padding: 3px 6px; border-radius: 4px; line-height: 1.4; }
  .toc a:hover { background: #18a058; color: #fff; }
  /* 必须放在 .toc a 的块级规则之后并用更高优先级覆盖，否则 folder 标题会被
     覆盖回 display:block，导致开关与文字各占一行（错位） */
  .toc li.toc-folder > a { display: inline; }
  .toc-root > a { font-weight: 600; }
  .toc-folder > a { font-weight: 600; color: #18a058; }
  .toc a:target, .toc a.current { background: #18a058; color: #fff; }
  /* 请求条目：方法徽标 + 名称横排（对齐客户端集合树 MethodTag：等宽小字、语义色，无底色）；
     item 10：hover / 选中时**整行**（含徽标）上绿底，名称反白；徽标做反色处理 ——
     白底胶囊 + 方法原色文字（各方法颜色不同，白底对全部语义色都有对比度） */
  .toc-request, .toc-doc { display: flex; align-items: center; gap: 6px; padding: 3px 6px; border-radius: 4px; }
  .toc-doc > a { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; background: none; padding: 0; }
  .toc-doc:hover, .toc-doc.selected { background: #18a058; }
  .toc-doc:hover > a, .toc-doc.selected > a { color: #fff; }
  .toc-doc:hover .tm, .toc-doc.selected .tm { background: #fff; border-radius: 3px; padding: 0 4px; }
  .toc-request > a { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; background: none; padding: 0; }
  .toc-request:hover, .toc-request.selected { background: #18a058; }
  .toc-request:hover > a, .toc-request.selected > a { color: #fff; }
  .toc-request:hover .tm, .toc-request.selected .tm { background: #fff; border-radius: 3px; padding: 0 4px; }
  .tm { flex: 0 0 auto; min-width: 34px; font-family: "JetBrains Mono", ui-monospace, Menlo, Consolas, monospace;
        font-size: 10px; font-weight: 600; letter-spacing: 0.4px; line-height: 16px; }
  .m-get { color: #18a058; }
  .m-post { color: #2080f0; }
  .m-put { color: #f0a020; }
  .m-delete { color: #d03050; }
  .m-patch { color: #8a2be2; }
  .m-grpc { color: #0e94a8; }
  .m-other { color: #8a9199; }
  .m-doc { color: #6b7280; }
  /* 正文 API 标题内的方法徽标：与标题文字拉开间距（TOC 里由 flex gap 控制，不叠加） */
  h1 .tm, h2 .tm, h3 .tm, h4 .tm, h5 .tm, h6 .tm { margin-right: 8px; }
  .content { flex: 1 1 auto; min-width: 0; }
  /* 当前请求的右侧大纲（item 9 正解：URL/参数/请求头/文档内容等小节，点击跳转）；
     内容由 JS 在单节展示时动态生成，不足两项时隐藏。
     外观与左侧目录同款卡片：四边完整描边 + 圆角 + 浅底，不再只靠一条左边线分隔。 */
  .ol { flex: 0 0 200px; position: sticky; top: 16px; max-height: calc(100vh - 32px); overflow: auto;
        font-size: 12px; padding: 10px 12px; border: 1px solid #e3e3e3; border-radius: 10px;
        background: #fafbfc; }
  .ol-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 6px; }
  .ol-title { font-weight: 600; color: #6b7280; }
  /* 收起按钮：标题右侧的小方块图标按钮（与目录卡片的轻量风格一致） */
  .ol-toggle { flex: 0 0 auto; width: 20px; height: 20px; padding: 0;
        display: inline-flex; align-items: center; justify-content: center;
        border: 1px solid #e3e3e3; border-radius: 5px; background: #fff; color: #6b7280;
        font-family: inherit; font-size: 13px; line-height: 1; cursor: pointer; }
  .ol-toggle:hover { border-color: #18a058; color: #18a058; background: #f0faf3; }
  .ol a { display: block; color: #555; text-decoration: none; padding: 2px 6px; border-radius: 4px;
          white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .ol a:hover { background: #f0faf3; color: #18a058; }
  /* 收起态：整块 display:none —— 既不占宽度，也不留半条边框；正文自动铺满。
     只有"有大纲且被手动收起"时才在右侧贴边露出展开把手（.ol-collapsed 由 JS 维护）。 */
  .layout.ol-collapsed .ol,
  .layout.ol-empty .ol { display: none; }
  .ol-open { position: fixed; right: 0; top: 50%; transform: translateY(-50%);
        display: none; align-items: center; justify-content: center;
        width: 22px; height: 56px; padding: 0; border: 1px solid #e3e3e3; border-right: none;
        border-radius: 8px 0 0 8px; background: #fff; color: #6b7280;
        font-family: inherit; font-size: 13px; line-height: 1; cursor: pointer;
        box-shadow: -1px 0 6px rgba(0, 0, 0, .06); }
  .ol-open:hover { border-color: #18a058; color: #18a058; background: #f0faf3; }
  .layout.ol-collapsed .ol-open { display: inline-flex; }
  h1,h2,h3,h4,h5,h6 { margin: 1.4em 0 0.6em; font-weight: 600; line-height: 1.3; scroll-margin-top: 16px; }
  h1 { font-size: 1.8em; border-bottom: 1px solid #e3e3e3; padding-bottom: 0.3em; }
  code { font-family: "JetBrains Mono", ui-monospace, Menlo, Consolas, monospace;
         background: #f5f6f8; padding: 0.15em 0.4em; border-radius: 4px; font-size: 0.92em; }
  pre { background: #f5f6f8; padding: 12px 14px; border-radius: 8px; overflow: auto; }
  pre code { background: none; padding: 0; }
  table { border-collapse: collapse; width: 100%; margin: 0.6em 0 1em; font-size: 13px; }
  th, td { border: 1px solid #e3e3e3; padding: 6px 10px; text-align: left; }
  th { background: #fafafa; font-weight: 500; }
  hr { border: none; border-top: 1px solid #e3e3e3; margin: 1.6em 0; }
  blockquote { margin: 0.6em 0; padding: 0.4em 1em; border-left: 3px solid #18a058; background: #f5f6f8; color: #555; }
  strong { font-weight: 600; }
  /* 窄屏：集合树排到正文上方、右侧大纲隐藏（小节跳转在窄屏价值低）。
     收起态的展开把手也一并隐藏，否则窄屏上会留一个点了没反应的浮标。 */
  @media (max-width: 720px) {
    /* 竖排后 align-items 从 flex-start 改回 stretch：否则内容列会按 max-content 收缩，
       出现"目录占满宽、正文只有半宽"的错位（flex-direction: column 时 cross axis 是宽度）。 */
    .layout { flex-direction: column; gap: 12px; padding: 16px 14px; align-items: stretch; }
    .toc { flex: 0 0 auto; position: static; max-height: none; width: 100%; }
    .ol, .layout.ol-collapsed .ol-open { display: none !important; }
  }
</style>
</head>
<body>
`

// htmlScriptBlock 交互脚本：目录搜索过滤、单节展示切换、当前项高亮与右侧大纲重建。
const htmlScriptBlock = `
<script>
(function(){
  var list = document.querySelector('.toc-list');
  var box = document.querySelector('.toc-search');
  if(!list || !box) return;

  // 折叠 / 展开：点击开关切换 aria-expanded，CSS 据此隐藏子列表（点击不会触发跳转）
  list.addEventListener('click', function(e){
    var t = e.target;
    if(t && t.classList && t.classList.contains('toc-toggle')){
      var li = t.parentElement;
      li.setAttribute('aria-expanded', li.getAttribute('aria-expanded') === 'true' ? 'false' : 'true');
    }
  });
  list.addEventListener('keydown', function(e){
    var t = e.target;
    if(t && t.classList && t.classList.contains('toc-toggle') && (e.key === 'Enter' || e.key === ' ')){
      e.preventDefault();
      t.click();
    }
  });

  // 搜索：隐藏不匹配条目，自动展开命中节点的祖先目录，命中目录时一并展开其子节点
  function reset(){
    var lis = list.querySelectorAll('li');
    for(var i=0;i<lis.length;i++){
      lis[i].style.display = '';
      if(lis[i].classList.contains('toc-folder')){
        lis[i].setAttribute('aria-expanded', lis[i].getAttribute('data-default') || 'false');
      }
    }
  }
  function filter(q){
    if(!q){ reset(); return; }
    q = q.toLowerCase();
    var all = list.querySelectorAll('li');
    var i, a, node, cur, ul, parent, k, desc;
    for(i=0;i<all.length;i++){ all[i].dataset.shown = ''; }
    for(i=0;i<all.length;i++){
      a = all[i].querySelector(':scope > a');
      if(a && a.textContent.toLowerCase().indexOf(q) !== -1){
        node = all[i];
        node.dataset.shown = '1';
        if(node.classList.contains('toc-folder')){
          node.setAttribute('aria-expanded', 'true');
          desc = node.querySelectorAll('li');
          for(k=0;k<desc.length;k++){ desc[k].dataset.shown = '1'; }
        }
        cur = node;
        while(cur){
          ul = cur.parentElement;
          parent = ul ? ul.parentElement : null;
          if(parent && parent !== list && parent.tagName === 'LI'){
            parent.dataset.shown = '1';
            if(parent.classList.contains('toc-folder')) parent.setAttribute('aria-expanded', 'true');
            cur = parent;
          } else { cur = null; }
        }
      }
    }
    for(i=0;i<all.length;i++){
      all[i].style.display = all[i].dataset.shown ? '' : 'none';
    }
  }
  box.addEventListener('input', function(){ filter(box.value.trim()); });

  // 单节展示：点击目录项时，右侧仅显示该项对应的 API（含其下子节点），其余隐藏。
  // 这样导出的 HTML 不再一屏铺满全部 API，而是像文档站一样「左侧选、右侧看一节」。
  var content = document.querySelector('.content');
  var tocLinks = list.querySelectorAll('.toc a');

  function clearCurrent(){
    for(var i=0;i<tocLinks.length;i++){ tocLinks[i].classList.remove('current'); }
    // 请求行整行选中态（item 10）：随行清除
    var rows = list.querySelectorAll('.toc-request.selected, .toc-doc.selected');
    for(var j=0;j<rows.length;j++){ rows[j].classList.remove('selected'); }
  }

  // ---- 当前请求的右侧大纲（item 9）：单节展示时把节内小节/生成标签收集成可点击目录 ----
  var layout = document.querySelector('.layout');
  var olNav = document.querySelector('.ol');
  var olList = olNav ? olNav.querySelector('.ol-list') : null;
  var olToggle = olNav ? olNav.querySelector('.ol-toggle') : null;
  var olOpen = document.querySelector('.ol-open');
  var olSeq = 0;
  // 用户手动收起后一直保持收起（切节重建大纲时也不自动弹回），直到点展开把手
  var olCollapsed = false;
  function olId(el){ if(!el.id){ el.id = 'ol-' + (++olSeq); } return el.id; }
  function olEsc(s){ return s.replace(/&/g,'&amp;').replace(/</g,'&lt;'); }
  // 大纲是否值得显示：至少两条（只有一条时它和当前标题重复，占一列反而碍事）
  function olHas(){ return !!(olList && olList.children.length > 1); }
  // 显示状态**只有一个出口**：面板显隐与展开把手都由 .layout 上的两个类决定，
  // 避免"手动收起"和"条目不足"两套逻辑各改一次 style 而互相覆盖。
  function olApply(){
    if(!layout) return;
    var has = olHas();
    layout.classList.toggle('ol-empty', !has);
    layout.classList.toggle('ol-collapsed', has && olCollapsed);
    if(olToggle){ olToggle.setAttribute('aria-expanded', has && !olCollapsed ? 'true' : 'false'); }
  }
  // buildOutline 扫描当前节的块级元素：标题（h*）与「以粗体标签开头的段落」（**参数** 等生成内容）
  // 都进大纲；紧跟标题的代码块是请求 URL，补一个合成条目。不足两项时整块隐藏。
  function buildOutline(nodes){
    if(!olList) return;
    var items = [];
    for(var i=0;i<nodes.length;i++){
      var el = nodes[i], tag = el.tagName;
      if(/^H[1-6]$/.test(tag)){
        items.push({el:el, text:el.textContent.trim(), lv:parseInt(tag.charAt(1),10)});
      } else if(tag === 'PRE' && i === 1){
        items.push({el:el, text:'URL', lv:0});
      } else if(tag === 'P' && el.firstElementChild && el.firstElementChild.tagName === 'STRONG'){
        items.push({el:el, text:el.textContent.trim().slice(0,24), lv:0});
      }
    }
    var html = '';
    for(var j=0;j<items.length;j++){
      var it = items[j];
      // 文档子标题按层级缩进（h3 一级、h4 二级），生成标签与节标题不缩进
      var pad = it.lv >= 3 ? ' style="padding-left:' + ((it.lv - 2) * 12 + 6) + 'px"' : '';
      html += '<a href="#' + olId(it.el) + '"' + pad + '>' + olEsc(it.text) + '</a>';
    }
    olList.innerHTML = html;
    olApply();
  }
  if(olNav){
    olNav.addEventListener('click', function(e){
      var a = e.target && e.target.closest ? e.target.closest('a') : null;
      if(!a) return;
      var href = a.getAttribute('href') || '';
      if(href.charAt(0) !== '#') return;
      e.preventDefault();
      var t = document.getElementById(href.slice(1));
      if(t) t.scrollIntoView({ behavior:'smooth', block:'start' });
    });
  }
  // 收起 / 展开右侧大纲（收起后正文自动铺满；展开把手贴在窗口右缘）
  if(olToggle){
    olToggle.addEventListener('click', function(){ olCollapsed = true; olApply(); });
  }
  if(olOpen){
    olOpen.addEventListener('click', function(){ olCollapsed = false; olApply(); });
  }

  // 仅展示 id 对应的那一节：从目标标题起，到下一个同级或更高级标题为止（含其间的所有子节点）。
  function showSection(id){
    if(!content) return;
    var target = document.getElementById(id);
    if(!target) return;
    var lvl = parseInt(target.tagName.charAt(1), 10); // 1..6
    var nodes = [];
    var sib = target;
    while(sib){
      nodes.push(sib);
      sib = sib.nextElementSibling;
      if(sib && sib.tagName.charAt(0) === 'H'){
        var nlvl = parseInt(sib.tagName.charAt(1), 10);
        if(nlvl <= lvl) break; // 遇到同级或更高级标题 => 超出本节范围
      }
    }
    var kids = content.children;
    for(var i=0;i<kids.length;i++){ kids[i].style.display = 'none'; }
    for(var j=0;j<nodes.length;j++){ nodes[j].style.display = ''; }
    buildOutline(nodes); // 右侧大纲跟着当前节重建
    clearCurrent();
    var cur = document.querySelector('.toc a[href="#' + id + '"]');
    if(cur){
      cur.classList.add('current');
      var row = cur.parentElement;
      if(row && row.classList.contains('toc-request')) row.classList.add('selected');
    }
    if(target.scrollIntoView) target.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  // 点击目录链接：目录(fodler)只在左侧展开/收起子树，右侧不展示其下 API；
  // 只有点击具体的 API(request) 才在右侧单节展示。
  // 集合名本身是正文里的 h1，点到它等于展示整篇文档（已无单独的全量展示按钮）。
  for(var li=0; li<tocLinks.length; li++){
    tocLinks[li].addEventListener('click', function(e){
      var href = this.getAttribute('href') || '';
      if(href.charAt(0) !== '#') return;
      e.preventDefault();
      var item = this.parentElement; // 该链接所在的 <li>
      if(item && item.classList.contains('toc-folder')){
        // 目录：仅切换 TOC 展开/折叠，便于展开后点选其下的 API；右侧内容不变
        var expanded = item.getAttribute('aria-expanded') === 'true';
        item.setAttribute('aria-expanded', expanded ? 'false' : 'true');
        return;
      }
      showSection(href.slice(1));
    });
  }

  // 默认进入单节展示：优先展示第一个 API（request），避免一屏铺满；无 API 时退回第一个标题
  var firstReq = list.querySelector('.toc-request a');
  if(firstReq && firstReq.getAttribute('href')){
    var fid = firstReq.getAttribute('href').slice(1);
    if(document.getElementById(fid)){ showSection(fid); }
  } else {
    var firstHeading = content ? content.querySelector('h2,h3,h4,h5,h6') : null;
    if(firstHeading && firstHeading.id){ showSection(firstHeading.id); }
  }
})();
</script>
</body>
</html>
`

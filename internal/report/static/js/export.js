// Export buttons for the report. Reads the same tree report.js already
// parsed (via window.SecurityHubReport) instead of re-parsing the JSON, and
// needs no network access, so it works whether the report is opened from
// disk (file://) or served over http.
(function () {
  "use strict";

  function flattenTree(node, rows) {
    rows.push(node);
    (node.children || []).forEach(function (child) { flattenTree(child, rows); });
    return rows;
  }

  function collectCheckNames(rows) {
    var names = {};
    rows.forEach(function (node) {
      Object.keys(node.checks || {}).forEach(function (name) { names[name] = true; });
    });
    return Object.keys(names).sort();
  }

  function csvField(value) {
    var str = value === undefined || value === null ? "" : String(value);
    if (/["\n,]/.test(str)) {
      str = '"' + str.replace(/"/g, '""') + '"';
    }
    return str;
  }

  function scoreValue(stat) {
    return stat ? stat.average.toFixed(1) : "";
  }

  function buildCSV(root) {
    var rows = flattenTree(root, []);
    var checkNames = collectCheckNames(rows);
    var header = ["Path", "Kind", "Projects", "Overall Score"].concat(checkNames);

    var lines = [header.map(csvField).join(",")];
    rows.forEach(function (node) {
      var line = [
        node.fullPath || node.name || "(root)",
        node.kind || "",
        node.projectCount || 0,
        scoreValue(node.score),
      ].concat(checkNames.map(function (name) { return scoreValue((node.checks || {})[name]); }));
      lines.push(line.map(csvField).join(","));
    });

    return lines.join("\n");
  }

  function downloadBlob(content, mimeType, fileName) {
    var blob = new Blob([content], { type: mimeType });
    var url = URL.createObjectURL(blob);
    var link = document.createElement("a");
    link.href = url;
    link.download = fileName;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  }

  function dateStamp() {
    return new Date().toISOString().slice(0, 10);
  }

  function exportCSV() {
    var root = window.SecurityHubReport.getRoot();
    downloadBlob(buildCSV(root), "text/csv;charset=utf-8;", "security-hub-report-" + dateStamp() + ".csv");
  }

  function exportPDF() {
    window.print();
  }

  document.getElementById("export-csv").addEventListener("click", exportCSV);
  document.getElementById("export-pdf").addEventListener("click", exportPDF);
})();

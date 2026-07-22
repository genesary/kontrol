// Renders the drill-down tree view of the report. Exposes the current
// navigation state on window.SecurityHubReport so export.js can read the
// same data without a second copy or a fetch (the report is opened via
// file:// as often as it is served, so no network requests are made).
(function () {
  "use strict";

  var GOOD_THRESHOLD = 8;
  var MID_THRESHOLD = 5;

  var root = JSON.parse(document.getElementById("report-data").textContent);
  var checkDocs = JSON.parse(document.getElementById("check-docs-data").textContent) || {};
  var path = [root];

  function scoreClass(stat) {
    if (!stat) return "none";
    if (stat.average >= GOOD_THRESHOLD) return "good";
    if (stat.average >= MID_THRESHOLD) return "mid";
    return "bad";
  }

  function scoreText(stat) {
    return stat ? stat.average.toFixed(1) : "N/A";
  }

  function el(tag, attrs, children) {
    var node = document.createElement(tag);
    attrs = attrs || {};
    Object.keys(attrs).forEach(function (key) {
      if (key === "text") {
        node.textContent = attrs[key];
      } else if (key === "class") {
        node.className = attrs[key];
      } else {
        node.setAttribute(key, attrs[key]);
      }
    });
    (children || []).forEach(function (child) { node.appendChild(child); });
    return node;
  }

  function renderBreadcrumb() {
    var nav = document.getElementById("breadcrumb");
    nav.innerHTML = "";

    path.forEach(function (node, index) {
      if (index > 0) {
        nav.appendChild(el("span", { class: "mx-1.5", text: "/" }));
      }

      var label = node.name || "root";
      if (index === path.length - 1) {
        nav.appendChild(el("span", { text: label }));

        return;
      }

      var link = el("a", { class: "breadcrumb-link", text: label });
      link.addEventListener("click", (function (depth) {
        return function () {
          path = path.slice(0, depth + 1);
          render();
        };
      })(index));
      nav.appendChild(link);
    });
  }

  function renderInfoIcon(name) {
    var doc = checkDocs[name];
    if (!doc) return null;

    var popoverChildren = [];
    var textParts = [];
    if (doc.short) {
      popoverChildren.push(el("strong", { text: doc.short }));
      textParts.push(doc.short);
    }
    if (doc.description) {
      popoverChildren.push(el("p", { text: doc.description }));
      textParts.push(doc.description);
    }
    if (doc.remediation && doc.remediation.length > 0) {
      var items = doc.remediation.map(function (step) { return el("li", { text: step }); });
      popoverChildren.push(el("ul", {}, items));
      textParts.push(doc.remediation.join("\n"));
    }
    if (doc.url) {
      popoverChildren.push(el("a", { href: doc.url, target: "_blank", rel: "noopener noreferrer", text: "Learn how to fix →" }));
      textParts.push(doc.url);
    }

    var popover = el("div", { class: "tooltip-popover", title: "Click to copy" }, popoverChildren);
    popover.addEventListener("click", function (event) {
      if (event.target.tagName === "A") {
        return;
      }
      event.stopPropagation();
      copyToClipboard(textParts.join("\n\n"), popover);
    });

    return el("span", { class: "info-icon", tabindex: "0", "aria-label": "About " + name, text: "i" }, [popover]);
  }

  function copyToClipboard(text, target) {
    function flash() {
      target.classList.add("copied");
      setTimeout(function () { target.classList.remove("copied"); }, 1200);
    }

    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(flash, function () { fallbackCopy(text, flash); });
    } else {
      fallbackCopy(text, flash);
    }
  }

  function fallbackCopy(text, done) {
    var textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    document.body.appendChild(textarea);
    textarea.select();
    try {
      document.execCommand("copy");
    } catch (err) {
      // Clipboard access unavailable; nothing more we can do here.
    }
    document.body.removeChild(textarea);
    done();
  }

  function renderSummary(node) {
    var summary = document.getElementById("summary");
    summary.innerHTML = "";

    var overallBadge = el("div", { class: "badge badge-lg badge-" + scoreClass(node.score), text: scoreText(node.score) });
    summary.appendChild(el("div", { class: "card overall-card" }, [
      overallBadge,
      el("div", { class: "card-label", text: (node.projectCount || 0) + " project(s)" }),
    ]));

    var checkNames = Object.keys(node.checks || {}).sort();
    if (checkNames.length > 0) {
      var rows = checkNames.map(function (name) {
        var stat = node.checks[name];

        var nameChildren = [document.createTextNode(name)];
        var icon = renderInfoIcon(name);
        if (icon) {
          nameChildren.push(icon);
        }

        return el("div", { class: "check-row" }, [
          el("span", { class: "check-name" }, nameChildren),
          el("span", { class: "badge badge-" + scoreClass(stat), text: scoreText(stat) }),
        ]);
      });

      summary.appendChild(el("div", { class: "card checks-card" }, [
        el("h2", { class: "checks-heading", text: "Checks" }),
      ].concat(rows)));
    }
  }

  function renderScanError(node) {
    var container = document.getElementById("scan-error");
    container.innerHTML = "";

    if (node.scanError) {
      container.appendChild(el("div", { class: "scan-error", text: "Scorecard analysis failed: " + node.scanError }));
    }
  }

  function renderChildren(node) {
    var container = document.getElementById("children");
    container.innerHTML = "";

    if (node.webUrl) {
      container.appendChild(el("p", { class: "mb-3" }, [
        el("a", { class: "web-link", href: node.webUrl, target: "_blank", rel: "noopener noreferrer", text: "Open in GitLab →" }),
      ]));
    }

    if (!node.children || node.children.length === 0) {
      return;
    }

    var table = el("table", { class: "children-table" });
    var head = el("tr", {}, [
      el("th", { text: "Name" }),
      el("th", { text: "Score" }),
      el("th", { text: "Projects" }),
    ]);
    table.appendChild(el("thead", {}, [head]));

    var body = el("tbody");
    node.children.forEach(function (child) {
      var icon = child.kind === "group" ? "📁" : "📄";
      var row = el("tr", { class: "row-clickable" }, [
        el("td", {}, [el("span", { class: "kind-icon", text: icon }), document.createTextNode(child.name)]),
        el("td", {}, [el("span", { class: "badge badge-" + scoreClass(child.score), text: scoreText(child.score) })]),
        el("td", { class: "count", text: String(child.projectCount || 0) }),
      ]);
      row.addEventListener("click", function () {
        path = path.concat([child]);
        render();
      });
      body.appendChild(row);
    });
    table.appendChild(body);

    container.appendChild(table);
  }

  function render() {
    var current = path[path.length - 1];
    renderBreadcrumb();
    renderSummary(current);
    renderScanError(current);
    renderChildren(current);
  }

  window.SecurityHubReport = {
    getRoot: function () { return root; },
    getCurrentNode: function () { return path[path.length - 1]; },
  };

  render();
})();

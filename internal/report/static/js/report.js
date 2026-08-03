// Renders the drill-down tree view of the report. Exposes the current
// navigation state and the shared score vocabulary (bands, rails, tree
// walks) on window.SecurityHubReport so print.js and export.js can read the
// same data without a second copy or a fetch (the report is opened via
// file:// as often as it is served, so no network requests are made).
(function () {
  "use strict";

  var GOOD_THRESHOLD = 8;
  var MID_THRESHOLD = 5;
  var MAX_SCORE = 10;
  var ATTENTION_LIMIT = 5;
  var FILTER_THRESHOLD = 8;

  var BANDS = {
    good: { key: "good", label: "Strong" },
    mid: { key: "mid", label: "Fair" },
    bad: { key: "bad", label: "At risk" },
    none: { key: "none", label: "Not assessed" },
  };

  var root = JSON.parse(document.getElementById("report-data").textContent);
  var checkDocs = JSON.parse(document.getElementById("check-docs-data").textContent) || {};
  var path = [root];
  var sort = { key: "score", dir: "asc" };
  var filter = "";

  // ---- Score vocabulary -------------------------------------------------

  function band(stat) {
    if (!stat) return BANDS.none;
    if (stat.average >= GOOD_THRESHOLD) return BANDS.good;
    if (stat.average >= MID_THRESHOLD) return BANDS.mid;
    return BANDS.bad;
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

  function svgIcon(paths, size) {
    var svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("width", size);
    svg.setAttribute("height", size);
    svg.setAttribute("viewBox", "0 0 16 16");
    svg.setAttribute("fill", "none");
    svg.setAttribute("stroke", "currentColor");
    svg.setAttribute("stroke-width", "1.4");
    svg.setAttribute("stroke-linejoin", "round");
    svg.setAttribute("aria-hidden", "true");
    paths.forEach(function (d) {
      var node = document.createElementNS("http://www.w3.org/2000/svg", "path");
      node.setAttribute("d", d);
      svg.appendChild(node);
    });
    return svg;
  }

  function kindIcon(kind) {
    var icon = kind === "group"
      ? svgIcon(["M1.75 3.75h4l1.5 2h7v6.5h-12.5z"], 14)
      : svgIcon(["M3.75 1.75h5l3.5 3.5v9h-8.5z", "M8.75 1.75v3.5h3.5"], 14);
    icon.setAttribute("class", "kind-icon");
    return icon;
  }

  // A score rail: the same 0-10 instrument everywhere in the report, with
  // hairline gaps at the two band thresholds so a reader can see where a
  // score sits relative to policy without reading the number.
  function rail(stat, large) {
    if (!stat) {
      return el("div", { class: "rail-empty", "aria-hidden": "true" });
    }

    var width = Math.max(0, Math.min(stat.average / MAX_SCORE, 1)) * 100;
    var track = el("div", {
      class: large ? "rail rail-lg" : "rail",
      role: "img",
      "aria-label": "Score " + scoreText(stat) + " out of 10, " + band(stat).label,
    }, [
      el("div", { class: "rail-fill is-" + band(stat).key, style: "width:" + width.toFixed(1) + "%" }),
      el("span", { class: "rail-tick", style: "left:" + (MID_THRESHOLD * 10) + "%" }),
      el("span", { class: "rail-tick", style: "left:" + (GOOD_THRESHOLD * 10) + "%" }),
    ]);

    return track;
  }

  function bandLabel(stat, large) {
    var info = band(stat);
    return el("span", { class: "band is-" + info.key + (large ? " band-lg" : "") }, [
      el("span", { class: "band-dot" }),
      el("span", { text: info.label }),
    ]);
  }

  function scoreValue(stat) {
    return el("span", {
      class: stat ? "check-value" : "check-value is-na",
      text: scoreText(stat),
    });
  }

  // ---- Tree helpers -----------------------------------------------------

  // eachProject visits every project leaf under node, passing the chain of
  // nodes from node down to that leaf so a caller can navigate to it.
  function eachProject(node, visit, trail) {
    var chain = (trail || []).concat([node]);

    if (node.kind === "project") {
      visit(node, chain);

      return;
    }

    (node.children || []).forEach(function (child) { eachProject(child, visit, chain); });
  }

  function projectsUnder(node) {
    var found = [];
    eachProject(node, function (project, chain) { found.push({ node: project, chain: chain }); });

    return found;
  }

  function countGroups(node) {
    var total = 0;
    (node.children || []).forEach(function (child) {
      if (child.kind !== "group") return;
      total += 1 + countGroups(child);
    });

    return total;
  }

  function bandCounts(projects) {
    var counts = { good: 0, mid: 0, bad: 0, none: 0 };
    projects.forEach(function (entry) { counts[band(entry.node.score).key] += 1; });

    return counts;
  }

  function checkNamesOf(node) {
    return Object.keys(node.checks || {});
  }

  // ---- Panels -----------------------------------------------------------

  function panel(title, note, body, headExtra) {
    var head = [el("h2", { class: "panel-title", text: title })];
    if (note) {
      head.push(el("p", { class: "panel-note", text: note }));
    }
    if (headExtra) {
      head.push(headExtra);
    }

    return el("section", { class: "panel" }, [
      el("div", { class: "panel-head" }, head),
      body,
    ]);
  }

  function renderBreadcrumb() {
    var nav = document.getElementById("breadcrumb");
    nav.innerHTML = "";

    path.forEach(function (node, index) {
      if (index > 0) {
        nav.appendChild(el("span", { class: "crumb-sep", text: "/" }));
      }

      var label = node.name || "Instance";
      if (index === path.length - 1) {
        nav.appendChild(el("span", { class: "crumb-current", "aria-current": "page", text: label }));

        return;
      }

      var link = el("button", { type: "button", class: "crumb-link", text: label });
      link.addEventListener("click", function () {
        path = path.slice(0, index + 1);
        filter = "";
        render();
      });
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
    popoverChildren.push(el("span", { class: "tooltip-hint", text: "Click to copy this guidance" }));

    var popover = el("div", { class: "tooltip-popover" }, popoverChildren);
    popover.addEventListener("click", function (event) {
      if (event.target.tagName === "A") {
        return;
      }
      event.stopPropagation();
      copyToClipboard(textParts.join("\n\n"), popover);
    });

    // A span rather than a button: the popover holds a link, and an anchor
    // nested in a button is both invalid and unclickable.
    return el("span", {
      class: "info-icon",
      tabindex: "0",
      role: "note",
      "aria-label": "What " + name + " measures",
      text: "i",
    }, [popover]);
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

  // railScale labels the rail's two policy thresholds where they actually
  // sit, so the gaps in every rail in the report have a stated meaning.
  function railScale() {
    return el("div", { class: "rail-scale" }, [
      el("span", { class: "scale-start", text: "0" }),
      el("span", { style: "left:" + (MID_THRESHOLD * 10) + "%", text: MID_THRESHOLD + " fair" }),
      el("span", { style: "left:" + (GOOD_THRESHOLD * 10) + "%", text: GOOD_THRESHOLD + " strong" }),
      el("span", { class: "scale-end", text: String(MAX_SCORE) }),
    ]);
  }

  function heroBlock(node) {
    var scaleRow = railScale();

    var subject = node.kind === "project" ? "this project" : node.projectCount + " project" + (node.projectCount === 1 ? "" : "s");

    return el("div", { class: "hero" }, [
      el("p", { class: "eyebrow", text: "Overall score" }),
      el("div", { class: "hero-figure" }, [
        el("span", { class: "hero-value", text: scoreText(node.score) }),
        el("span", { class: "hero-scale", text: "/ 10" }),
      ]),
      bandLabel(node.score, true),
      el("div", { class: "hero-rail" }, [rail(node.score, true), scaleRow]),
      el("p", { class: "hero-note", text: node.kind === "project" ? "Weighted across the checks run on this project" : "Weighted average across " + subject }),
    ]);
  }

  // distributionBlock renders one 100% stacked bar of band counts: projects
  // per band for a group, checks per band for a single project, so the same
  // shape answers "how much of this is at risk" at either level.
  function distributionBlock(counts, total, title, unit) {
    var order = ["bad", "mid", "good", "none"];

    var stack = el("div", { class: "stack", role: "img", "aria-label": "Score band distribution across " + total + " " + unit });
    order.forEach(function (key) {
      if (counts[key] === 0) return;
      stack.appendChild(el("div", {
        class: "stack-seg is-" + key,
        style: "width:" + ((counts[key] / total) * 100).toFixed(2) + "%",
      }));
    });

    var legend = el("ul", { class: "stack-legend" });
    order.forEach(function (key) {
      legend.appendChild(el("li", {}, [
        el("span", { class: "band is-" + key }, [el("span", { class: "band-dot" })]),
        el("span", { class: "legend-count", text: String(counts[key]) }),
        el("span", { text: BANDS[key].label }),
      ]));
    });

    return el("div", { class: "aside-block" }, [
      el("p", { class: "eyebrow", text: title }),
      stack,
      legend,
    ]);
  }

  function checkBandCounts(node) {
    var counts = { good: 0, mid: 0, bad: 0, none: 0 };
    checkNamesOf(node).forEach(function (name) { counts[band(node.checks[name]).key] += 1; });

    return counts;
  }

  function fact(value, label) {
    return el("div", { class: "fact" }, [
      el("div", { class: "fact-value", text: value }),
      el("div", { class: "fact-label", text: label }),
    ]);
  }

  function factsBlock(node, projects) {
    var names = checkNamesOf(node);
    var assessed = names.filter(function (name) { return Boolean(node.checks[name]); }).length;
    var failed = projects.filter(function (entry) { return Boolean(entry.node.scanError); }).length;

    var facts = [];
    if (node.kind === "project") {
      facts.push(fact(assessed + " of " + names.length, "checks assessed"));
    } else {
      facts.push(fact(String(node.projectCount || 0), "projects"));
      facts.push(fact(String(countGroups(node)), node.kind === "group" ? "subgroups" : "groups"));
      facts.push(fact(assessed + " of " + names.length, "checks assessed"));
    }
    if (failed > 0) {
      facts.push(fact(String(failed), failed === 1 ? "project not analyzed" : "projects not analyzed"));
    }

    var children = [el("p", { class: "eyebrow", text: "Scope" }), el("div", { class: "facts" }, facts)];

    if (node.webUrl) {
      children.push(el("p", {}, [
        el("a", { class: "link", href: node.webUrl, target: "_blank", rel: "noopener noreferrer", text: "Open in GitLab →" }),
      ]));
    }

    return el("div", { class: "aside-block" }, children);
  }

  function renderSummary(node) {
    var container = document.getElementById("summary");
    container.innerHTML = "";

    var projects = projectsUnder(node);
    var asideBlocks = [];
    var checkCount = checkNamesOf(node).length;

    if (node.kind === "project") {
      if (checkCount > 0) {
        asideBlocks.push(distributionBlock(checkBandCounts(node), checkCount, "Checks by score band", "checks"));
      }
    } else if (projects.length > 0) {
      asideBlocks.push(distributionBlock(bandCounts(projects), projects.length, "Projects by score band", "projects"));
    }

    asideBlocks.push(factsBlock(node, projects));

    container.appendChild(el("section", { class: "panel" }, [
      el("div", { class: "summary-grid" }, [
        heroBlock(node),
        el("div", { class: "aside" }, asideBlocks),
      ]),
    ]));

    container.appendChild(renderChecks(node));

    var attention = renderAttention(node, projects);
    if (attention) {
      container.appendChild(attention);
    }
  }

  function renderChecks(node) {
    var names = checkNamesOf(node);
    if (names.length === 0) {
      return el("section", { class: "panel" }, [
        el("div", { class: "empty-note", text: "No Scorecard checks were recorded here." }),
      ]);
    }

    // Weakest first: that is the order a reader acts on. Checks nothing
    // could be assessed for sink to the bottom rather than reading as zero.
    names.sort(function (a, b) {
      var left = node.checks[a];
      var right = node.checks[b];
      if (!left && !right) return a.localeCompare(b);
      if (!left) return 1;
      if (!right) return -1;
      if (left.average !== right.average) return left.average - right.average;

      return a.localeCompare(b);
    });

    var list = el("div", { class: "check-list" });
    names.forEach(function (name) {
      var stat = node.checks[name];
      var label = el("div", { class: "check-name" }, [el("span", { text: name })]);
      var icon = renderInfoIcon(name);
      if (icon) {
        label.appendChild(icon);
      }

      list.appendChild(el("div", { class: "check-row" }, [
        label,
        el("div", { class: "rail-cell" }, [rail(stat)]),
        scoreValue(stat),
        bandLabel(stat),
      ]));
    });

    var note = node.kind === "project"
      ? "Weakest first"
      : "Weakest first · averaged across " + node.projectCount + " project" + (node.projectCount === 1 ? "" : "s");

    return panel("Posture by check", note, el("div", { class: "panel-body" }, [list]));
  }

  function renderAttention(node, projects) {
    if (node.kind === "project" || projects.length < 2) {
      return null;
    }

    var ranked = projects.filter(function (entry) { return Boolean(entry.node.score); });
    if (ranked.length === 0) {
      return null;
    }

    ranked.sort(function (a, b) { return a.node.score.average - b.node.score.average; });
    ranked = ranked.slice(0, ATTENTION_LIMIT);

    var list = el("div", { class: "attention" });
    ranked.forEach(function (entry) {
      var row = el("button", { type: "button", class: "attention-row" }, [
        el("div", {}, [
          el("div", { class: "attention-name", text: entry.node.name }),
          el("div", { class: "attention-path", text: entry.node.fullPath || entry.node.name }),
        ]),
        rail(entry.node.score),
        scoreValue(entry.node.score),
      ]);
      row.addEventListener("click", function () {
        path = path.slice(0, path.length - 1).concat(entry.chain);
        filter = "";
        render();
      });
      list.appendChild(row);
    });

    return panel(
      "Start here",
      "Lowest scoring projects in this scope",
      el("div", { class: "panel-body" }, [list])
    );
  }

  function renderScanError(node) {
    var container = document.getElementById("scan-error");
    container.innerHTML = "";

    if (!node.scanError) {
      return;
    }

    container.appendChild(el("div", { class: "scan-error" }, [
      el("div", {}, [
        el("strong", { text: "Scorecard could not analyze this project" }),
        el("code", { text: node.scanError }),
      ]),
    ]));
  }

  function sortedChildren(node) {
    var children = (node.children || []).slice();

    if (filter) {
      var needle = filter.toLowerCase();
      children = children.filter(function (child) {
        return (child.name || "").toLowerCase().indexOf(needle) !== -1
          || (child.fullPath || "").toLowerCase().indexOf(needle) !== -1;
      });
    }

    var direction = sort.dir === "asc" ? 1 : -1;
    children.sort(function (a, b) {
      if (sort.key === "name") {
        return direction * (a.name || "").localeCompare(b.name || "");
      }
      if (sort.key === "count") {
        return direction * ((a.projectCount || 0) - (b.projectCount || 0));
      }

      // Unscored children sort last whichever direction is active: they are
      // not "the worst", they are unknown.
      if (!a.score && !b.score) return (a.name || "").localeCompare(b.name || "");
      if (!a.score) return 1;
      if (!b.score) return -1;

      return direction * (a.score.average - b.score.average);
    });

    return children;
  }

  function sortableHeader(label, key, extraClass) {
    var active = sort.key === key;
    var button = el("button", { type: "button", class: "th-btn", text: label });
    if (active) {
      button.appendChild(el("span", { class: "sort-caret", text: sort.dir === "asc" ? "↑" : "↓" }));
    }
    button.addEventListener("click", function () {
      if (sort.key === key) {
        sort.dir = sort.dir === "asc" ? "desc" : "asc";
      } else {
        sort = { key: key, dir: key === "name" ? "asc" : "desc" };
      }
      renderChildren(path[path.length - 1]);
    });

    var header = el("th", {
      class: extraClass || "",
      "aria-sort": active ? (sort.dir === "asc" ? "ascending" : "descending") : "none",
    }, [button]);

    return header;
  }

  function renderChildren(node) {
    var container = document.getElementById("children");
    container.innerHTML = "";

    if (!node.children || node.children.length === 0) {
      return;
    }

    var children = sortedChildren(node);

    var table = el("table", { class: "data-table" });
    table.appendChild(el("thead", {}, [
      el("tr", {}, [
        sortableHeader("Name", "name"),
        sortableHeader("Score", "score"),
        el("th", { text: "Band" }),
        sortableHeader("Projects", "count", "num"),
      ]),
    ]));

    var body = el("tbody");
    children.forEach(function (child) {
      var row = el("tr", { class: "row-clickable", tabindex: "0", role: "link" }, [
        el("td", {}, [
          el("div", { class: "row-name" }, [
            kindIcon(child.kind),
            el("span", { text: child.name }),
          ]),
        ]),
        el("td", {}, [
          el("div", { class: "score-cell" }, [rail(child.score), scoreValue(child.score)]),
        ]),
        el("td", {}, [bandLabel(child.score)]),
        el("td", { class: "num", text: String(child.projectCount || 0) }),
      ]);

      function open() {
        path = path.concat([child]);
        filter = "";
        render();
      }

      row.addEventListener("click", open);
      row.addEventListener("keydown", function (event) {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          open();
        }
      });
      body.appendChild(row);
    });
    table.appendChild(body);

    var headExtra = null;
    if ((node.children || []).length > FILTER_THRESHOLD) {
      var input = el("input", {
        type: "search",
        class: "table-filter",
        placeholder: "Filter by name",
        "aria-label": "Filter groups and projects by name",
        value: filter,
      });
      input.addEventListener("input", function () {
        filter = input.value;
        renderChildren(node);
        var next = document.querySelector(".table-filter");
        if (next) {
          next.focus();
          next.setSelectionRange(next.value.length, next.value.length);
        }
      });
      headExtra = input;
    }

    var count = children.length === (node.children || []).length
      ? children.length + " entries"
      : children.length + " of " + node.children.length + " entries";

    container.appendChild(panel(
      "Groups and projects",
      count,
      el("div", { class: "table-wrap" }, [table]),
      headExtra
    ));
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
    getCheckDocs: function () { return checkDocs; },
    thresholds: { good: GOOD_THRESHOLD, mid: MID_THRESHOLD, max: MAX_SCORE },
    bands: BANDS,
    band: band,
    scoreText: scoreText,
    el: el,
    rail: rail,
    bandLabel: bandLabel,
    railScale: railScale,
    eachProject: eachProject,
    projectsUnder: projectsUnder,
    countGroups: countGroups,
    bandCounts: bandCounts,
  };

  render();
})();

// Builds the printed/PDF version of the report. Printing the screen app
// directly would capture whatever the reader happened to have drilled into;
// instead this assembles a paginated dossier covering the whole instance:
// cover, executive summary, posture by check, group rollup, a per-project
// check matrix and any projects that could not be analyzed. It runs on
// beforeprint, so both the Export PDF button and the browser's own print
// command produce the same document.
(function () {
  "use strict";

  var api = window.SecurityHubReport;
  var el = api.el;
  var rail = api.rail;
  var band = api.band;
  var scoreText = api.scoreText;

  var MATRIX_CODE_LENGTH = 3;

  function meta(name) {
    var tag = document.querySelector('meta[name="' + name + '"]');

    return tag ? tag.getAttribute("content") : "";
  }

  function section(title, note, blocks, extraClass) {
    var children = [el("h2", { class: "print-section-title", text: title })];
    if (note) {
      children.push(el("p", { class: "print-section-note", text: note }));
    }

    return el("section", {
      class: "print-section" + (extraClass ? " " + extraClass : ""),
    }, children.concat(blocks));
  }

  function table(headers, rows, extraClass) {
    var head = el("tr", {}, headers.map(function (header) {
      return el("th", { class: header.class || "", text: header.label });
    }));

    var body = el("tbody", {}, rows);

    return el("table", { class: "print-table" + (extraClass ? " " + extraClass : "") }, [
      el("thead", {}, [head]),
      body,
    ]);
  }

  function scoreCells(stat) {
    return [
      el("td", { style: "width:22%" }, [rail(stat)]),
      el("td", { class: "num", style: "width:7%", text: scoreText(stat) }),
      el("td", { style: "width:12%" }, [api.bandLabel(stat)]),
    ];
  }

  // ---- Cover ------------------------------------------------------------

  function coverBlock(root, instance, generated) {
    var entries = [
      ["Instance", instance || "GitLab"],
      ["Generated", generated],
      ["Scope", (root.projectCount || 0) + " projects · " + api.countGroups(root) + " groups"],
      ["Method", "OpenSSF Scorecard, project-count weighted"],
    ];

    var list = el("dl", { class: "print-meta" });
    entries.forEach(function (entry) {
      list.appendChild(el("div", {}, [
        el("dt", { text: entry[0] }),
        el("dd", { text: entry[1] }),
      ]));
    });

    return el("header", { class: "print-cover" }, [
      el("p", { class: "eyebrow", text: "OpenSSF Scorecard assessment" }),
      el("h1", { class: "print-title", text: "Security posture report" }),
      list,
    ]);
  }

  // ---- Executive summary ------------------------------------------------

  function summarySection(root) {
    var projects = api.projectsUnder(root);
    var counts = api.bandCounts(projects);
    var total = projects.length || 1;

    var headline = el("div", { class: "print-headline" }, [
      el("div", {}, [
        el("p", { class: "eyebrow", text: "Overall score" }),
        el("div", { class: "hero-figure" }, [
          el("span", { class: "hero-value", text: scoreText(root.score) }),
          el("span", { class: "hero-scale", text: "/ 10" }),
        ]),
        api.bandLabel(root.score, true),
      ]),
      el("div", { class: "print-headline-rail" }, [
        rail(root.score, true),
        api.railScale(),
      ]),
    ]);

    var rows = ["bad", "mid", "good", "none"].map(function (key) {
      var share = ((counts[key] / total) * 100).toFixed(0) + "%";

      return el("tr", {}, [
        el("td", {}, [api.bandLabel(key === "none" ? null : { average: bandMidpoint(key), count: 1 })]),
        el("td", { class: "num", text: String(counts[key]) }),
        el("td", { class: "num", text: share }),
        el("td", { text: bandMeaning(key) }),
      ]);
    });

    var distribution = table([
      { label: "Band" },
      { label: "Projects", class: "num" },
      { label: "Share", class: "num" },
      { label: "What it means" },
    ], rows);

    return section("Executive summary", null, [
      headline,
      distribution,
      el("p", { class: "print-section-note", style: "margin-top:0.6rem", text: readingNote() }),
    ]);
  }

  function bandMidpoint(key) {
    if (key === "good") return api.thresholds.max;
    if (key === "mid") return api.thresholds.mid;

    return 0;
  }

  function bandMeaning(key) {
    if (key === "good") return "Scores " + api.thresholds.good + ".0 and above";
    if (key === "mid") return "Scores " + api.thresholds.mid + ".0 to " + (api.thresholds.good - 0.1).toFixed(1);
    if (key === "bad") return "Scores below " + api.thresholds.mid + ".0";

    return "No check could be scored";
  }

  function readingNote() {
    return "Every score runs from 0 to 10. A group's score is the average of its projects, weighted by how many projects sit beneath it, "
      + "so a large team moves the number more than a small one. Checks that could not be run appear as N/A and are excluded from the averages.";
  }

  // ---- Posture by check -------------------------------------------------

  function checkSection(root) {
    var names = Object.keys(root.checks || {});
    if (names.length === 0) {
      return null;
    }

    names.sort(function (a, b) {
      var left = root.checks[a];
      var right = root.checks[b];
      if (!left && !right) return a.localeCompare(b);
      if (!left) return 1;
      if (!right) return -1;
      if (left.average !== right.average) return left.average - right.average;

      return a.localeCompare(b);
    });

    var docs = api.getCheckDocs();
    var rows = names.map(function (name) {
      var stat = root.checks[name];
      var doc = docs[name];

      return el("tr", {}, [
        el("td", {}, [
          el("div", { style: "font-weight:600", text: name }),
          doc && doc.short ? el("div", { class: "print-sub", text: doc.short }) : el("span"),
        ]),
      ].concat(scoreCells(stat), [
        el("td", { class: "num", text: stat ? String(stat.count) : "—" }),
      ]));
    });

    return section(
      "Posture by check",
      "Instance-wide average per Scorecard check, weakest first.",
      [table([
        { label: "Check" },
        { label: "Score" },
        { label: "Value", class: "num" },
        { label: "Band" },
        { label: "Projects scored", class: "num" },
      ], rows)]
    );
  }

  // ---- Group rollup -----------------------------------------------------

  function groupSection(root) {
    var rows = [];

    (function walk(node, depth) {
      (node.children || []).forEach(function (child) {
        if (child.kind !== "group") return;

        rows.push(el("tr", {}, [
          el("td", {}, [
            el("span", { style: "display:inline-block;width:" + (depth * 10) + "px" }),
            el("span", { style: "font-weight:600", text: child.name }),
            el("div", { class: "path", text: child.fullPath || "" }),
          ]),
          el("td", { class: "num", text: String(child.projectCount || 0) }),
        ].concat(scoreCells(child.score))));

        walk(child, depth + 1);
      });
    })(root, 0);

    if (rows.length === 0) {
      return null;
    }

    return section(
      "Group rollup",
      "Every group and subgroup, in instance order. Scores are weighted by project count.",
      [table([
        { label: "Group" },
        { label: "Projects", class: "num" },
        { label: "Score" },
        { label: "Value", class: "num" },
        { label: "Band" },
      ], rows)],
      "page-break"
    );
  }

  // ---- Project matrix ---------------------------------------------------

  // checkCodes abbreviates each check to a short column header ("BP" for
  // Branch-Protection), extending an abbreviation only as far as it takes to
  // keep every code unique. A legend under the matrix spells them all out.
  function checkCodes(names) {
    var codes = {};
    var used = {};

    names.forEach(function (name) {
      var words = name.split(/[-_\s]+/).filter(Boolean);
      var base = words.length > 1
        ? words.map(function (word) { return word.charAt(0).toUpperCase(); }).join("")
        : name.slice(0, MATRIX_CODE_LENGTH).toUpperCase();

      var code = base;
      var extra = 1;
      while (used[code]) {
        code = base + (words[words.length - 1] || name).charAt(extra).toUpperCase();
        extra += 1;
        if (extra > name.length) {
          code = base + extra;
        }
      }

      used[code] = true;
      codes[name] = code;
    });

    return codes;
  }

  function matrixSection(root) {
    var projects = api.projectsUnder(root).map(function (entry) { return entry.node; });
    if (projects.length === 0) {
      return null;
    }

    var names = {};
    projects.forEach(function (project) {
      Object.keys(project.checks || {}).forEach(function (name) { names[name] = true; });
    });
    var checkNames = Object.keys(names).sort();
    var codes = checkCodes(checkNames);

    projects.sort(function (a, b) {
      if (!a.score && !b.score) return (a.fullPath || "").localeCompare(b.fullPath || "");
      if (!a.score) return 1;
      if (!b.score) return -1;
      if (a.score.average !== b.score.average) return a.score.average - b.score.average;

      return (a.fullPath || "").localeCompare(b.fullPath || "");
    });

    var headers = [
      { label: "Project" },
      { label: "Overall", class: "num" },
    ].concat(checkNames.map(function (name) {
      return { label: codes[name], class: "code" };
    }));

    var rows = projects.map(function (project) {
      var cells = [
        el("td", {}, [
          el("div", { style: "font-weight:600", text: project.name }),
          el("div", { class: "path", text: project.fullPath || "" }),
        ]),
        el("td", { class: "num cell-" + band(project.score).key, style: "font-weight:700", text: scoreText(project.score) }),
      ];

      checkNames.forEach(function (name) {
        var stat = (project.checks || {})[name];
        cells.push(el("td", {
          class: "cell cell-" + band(stat).key,
          text: stat ? stat.average.toFixed(1) : "N/A",
        }));
      });

      return el("tr", {}, cells);
    });

    var legend = el("ul", { class: "print-legend" });
    checkNames.forEach(function (name) {
      var item = el("li", {}, [
        el("code", { text: codes[name] }),
        el("span", { text: " " + name }),
      ]);
      legend.appendChild(item);
    });

    var key = el("div", { class: "print-key" }, [
      api.bandLabel({ average: api.thresholds.max, count: 1 }),
      api.bandLabel({ average: api.thresholds.mid, count: 1 }),
      api.bandLabel({ average: 0, count: 1 }),
      api.bandLabel(null),
    ]);

    return section(
      "Project detail",
      "Every project scored on every check, lowest overall score first. Cell shading repeats the score band.",
      [key, table(headers, rows, "matrix-table"), legend],
      "page-break landscape"
    );
  }

  // ---- Projects that could not be analyzed ------------------------------

  function failureSection(root) {
    var failed = api.projectsUnder(root)
      .map(function (entry) { return entry.node; })
      .filter(function (project) { return Boolean(project.scanError); });

    if (failed.length === 0) {
      return null;
    }

    var rows = failed.map(function (project) {
      return el("tr", {}, [
        el("td", {}, [
          el("div", { style: "font-weight:600", text: project.name }),
          el("div", { class: "path", text: project.fullPath || "" }),
        ]),
        el("td", { text: project.scanError }),
      ]);
    });

    return section(
      "Projects that could not be analyzed",
      "These projects are excluded from every average above, so the report neither rewards nor penalizes them.",
      [table([{ label: "Project" }, { label: "Reason" }], rows)],
      "page-break"
    );
  }

  // ---- Assembly ---------------------------------------------------------

  function build() {
    var root = api.getRoot();
    var instance = meta("report:instance");
    var generated = meta("report:generated");

    var container = document.getElementById("print-doc");
    container.innerHTML = "";

    container.appendChild(coverBlock(root, instance, generated));
    container.appendChild(summarySection(root));

    [checkSection(root), groupSection(root), matrixSection(root), failureSection(root)]
      .forEach(function (node) {
        if (node) {
          container.appendChild(node);
        }
      });
  }

  window.addEventListener("beforeprint", build);

  if (window.location.search.indexOf("preview=print") !== -1) {
    document.documentElement.setAttribute("data-preview", "print");
    build();
  }

  window.SecurityHubPrint = { build: build };
})();

/* Groadmap task board narrowing: the search term and the three header filters.
 *
 * The board carries every card of the roadmap, so narrowing it is a DOM
 * operation: the script shows and hides cards, recomputes each column's count,
 * toggles the empty states, and keeps the URL in step. No request is made, and
 * nothing is written anywhere (SPEC/WEB.md § Roadmap Tasks Page, Header search
 * control; Header filter controls; Effect on the board).
 *
 * ONE FILTERING MODEL, NOT TWO. The term and the three filters are not separate
 * mechanisms: each is one criterion, a card is shown when it satisfies EVERY
 * active criterion, and that single conjunction is computed in one place
 * (`matches` below) exactly as the server computes it in one place
 * (web.taskView.matches). Narrowing a criterion can only shrink the shown set,
 * and no criterion ever re-admits a card another excluded (SPEC/WEB.md § Roadmap
 * Tasks Page, How the criteria compose).
 *
 * SERVER AND CLIENT MUST AGREE. The same controls produce the same board whether
 * they were set here or carried in the URL of a cold load, and that equivalence
 * is kept by construction rather than by two implementations happening to match:
 *
 *   - a task's own values are transformed by ONE side only. The server folds the
 *     title once into data-search and writes the type, priority and severity once
 *     into data-type, data-priority and data-severity; this script only reads
 *     them, so it can never fold or spell a task's value differently;
 *   - the reference is rebuilt from data-task-id as "#<id>", exactly as the
 *     server builds it;
 *   - a filter value is never parsed here either: the only values these controls
 *     can hold are the options the server emitted from the TaskType enum and from
 *     the threshold range, so the "is this value accepted?" question the server
 *     answers for a URL parameter cannot arise on this side;
 *   - the TERM is the one value both sides normalise, and normalising it is TWO
 *     steps — strip the whitespace at its ends, then fold its case. This script
 *     takes NEITHER step from the JavaScript platform: it strips by the SERVER'S
 *     OWN whitespace set and folds by the SERVER'S OWN mapping, shipped to the
 *     page as SPACE_TABLE and FOLD_TABLE below, in that order.
 *
 *     Both platform functions would break the equivalence, and each on its own.
 *     The platform's case conversion is Unicode's Default Case Conversion rather
 *     than the simple mapping the folding rule fixes. The platform's own trimming
 *     removes a different set from the White_Space property the trim rule fixes:
 *     it keeps U+0085 (NEXT LINE), which carries the property, and it removes
 *     U+FEFF (ZERO WIDTH NO-BREAK SPACE), which does not. Both read tables of
 *     whatever Unicode version the browser happens to ship — so consulting either
 *     would let the same term select different tasks here than on the server, and
 *     different tasks in two browsers. The whitespace half would break it
 *     QUIETLY: the two sets agree on every code point but those two, so every
 *     ordinary term would go on agreeing and hide the disagreement (SPEC/WEB.md
 *     § Roadmap Tasks Page, The trim rule; The folding rule; One rule, and only
 *     one implementation of it).
 *
 * SECURITY. The term is text the user typed, echoed back into the page. It is
 * written through textContent only — this file contains no innerHTML, no
 * outerHTML, no insertAdjacentHTML and no document.write — so a term carrying
 * markup renders as visible characters and introduces no element, attribute or
 * script (SPEC/WEB.md § Roadmap Tasks Page, Escaping the term).
 */
(function () {
  "use strict";

  var input = document.querySelector('[data-role="task-search"]');
  var board = document.querySelector('[data-role="task-board"]');
  if (!input || !board) {
    return;
  }

  var boardEmpty = document.querySelector('[data-role="task-search-empty"]');
  var boardEmptyTerm = document.querySelector('[data-role="task-search-term"]');
  var boardEmptyTermPhrase = document.querySelector('[data-role="task-search-term-phrase"]');
  var columns = board.querySelectorAll('[data-role="task-board-column"]');

  /* equals is the type filter's comparison: a task matches when its type IS the
   * selected TaskType value. Both sides are the enum's own spelling — the card's
   * from the server, the control's from the option set the server emitted — so
   * the comparison is exact and needs no case folding, exactly as `rmp task list
   * -y` compares. */
  function equals(cardValue, filterValue) {
    return cardValue === filterValue;
  }

  /* atLeast is the priority and severity filters' comparison: a task matches when
   * its value is greater than or equal to the selected threshold, which is the
   * meaning `rmp task list -p` and `--severity` already carry. Both operands are
   * the server's own digit strings. */
  function atLeast(cardValue, filterValue) {
    return Number(cardValue) >= Number(filterValue);
  }

  /* The three filters in ONE table: the URL parameter each travels in, the
   * control that sets it, the card attribute it reads, and how it compares.
   * Nothing else in this file knows how many dimensions there are, so a further
   * one is a row here and no change anywhere else — which is the same property
   * the server's single conjunction has. */
  var filters = [
    {
      param: "type",
      control: document.querySelector('[data-role="task-filter-type"]'),
      attribute: "data-type",
      compare: equals
    },
    {
      param: "priority",
      control: document.querySelector('[data-role="task-filter-priority"]'),
      attribute: "data-priority",
      compare: atLeast
    },
    {
      param: "severity",
      control: document.querySelector('[data-role="task-filter-severity"]'),
      attribute: "data-severity",
      compare: atLeast
    }
  ];

  /* THE TERM'S NORMALISATION, SHIPPED FROM THE SERVER.
   *
   * The two tables below are the two halves of it, and both are run-encoded as a
   * flat array of consecutive spans, ordered by start and pairwise disjoint, so
   * that one binary search answers for either.
   *
   * FOLD_TABLE carries Unicode's SIMPLE lowercase mapping — the single
   * replacement code point the Unicode Character Database gives a code point,
   * applied to each code point on its own — as `start, length, delta` triples: a
   * code point c in [start, start + length) folds to c + delta, and a code point
   * in no run folds to itself.
   *
   * SPACE_TABLE carries Unicode's White_Space property, the set the trim rule
   * strips from a term's two ends, as `start, length` PAIRS: membership is the
   * whole question a trim asks, so a span needs no third number. A code point in
   * a span carries the property; one in none does not.
   *
   * Both are GENERATED from the server's own foldSearch and isSearchSpace by
   * internal/web/searchtables_gen.go (`go generate ./internal/web/`) and both are
   * checked against those two functions, over every code point of Unicode, by the
   * one Go test TestTaskSearchScript_ShippedRuleIsTheServerRule. A term's
   * normalisation is therefore the server's own answer on both paths by
   * construction, rather than by two implementations happening to agree. DO NOT
   * EDIT EITHER TABLE BY HAND. */
  /* BEGIN GENERATED FOLD TABLE */
  var FOLD_TABLE = [
    65,26,32, 192,23,32, 216,7,32, 256,1,1, 258,1,1, 260,1,1, 262,1,1, 264,1,1, 266,1,1,
    268,1,1, 270,1,1, 272,1,1, 274,1,1, 276,1,1, 278,1,1, 280,1,1, 282,1,1, 284,1,1,
    286,1,1, 288,1,1, 290,1,1, 292,1,1, 294,1,1, 296,1,1, 298,1,1, 300,1,1, 302,1,1,
    304,1,-199, 306,1,1, 308,1,1, 310,1,1, 313,1,1, 315,1,1, 317,1,1, 319,1,1, 321,1,1,
    323,1,1, 325,1,1, 327,1,1, 330,1,1, 332,1,1, 334,1,1, 336,1,1, 338,1,1, 340,1,1,
    342,1,1, 344,1,1, 346,1,1, 348,1,1, 350,1,1, 352,1,1, 354,1,1, 356,1,1, 358,1,1,
    360,1,1, 362,1,1, 364,1,1, 366,1,1, 368,1,1, 370,1,1, 372,1,1, 374,1,1, 376,1,-121,
    377,1,1, 379,1,1, 381,1,1, 385,1,210, 386,1,1, 388,1,1, 390,1,206, 391,1,1, 393,2,205,
    395,1,1, 398,1,79, 399,1,202, 400,1,203, 401,1,1, 403,1,205, 404,1,207, 406,1,211,
    407,1,209, 408,1,1, 412,1,211, 413,1,213, 415,1,214, 416,1,1, 418,1,1, 420,1,1,
    422,1,218, 423,1,1, 425,1,218, 428,1,1, 430,1,218, 431,1,1, 433,2,217, 435,1,1, 437,1,1,
    439,1,219, 440,1,1, 444,1,1, 452,1,2, 453,1,1, 455,1,2, 456,1,1, 458,1,2, 459,1,1,
    461,1,1, 463,1,1, 465,1,1, 467,1,1, 469,1,1, 471,1,1, 473,1,1, 475,1,1, 478,1,1,
    480,1,1, 482,1,1, 484,1,1, 486,1,1, 488,1,1, 490,1,1, 492,1,1, 494,1,1, 497,1,2,
    498,1,1, 500,1,1, 502,1,-97, 503,1,-56, 504,1,1, 506,1,1, 508,1,1, 510,1,1, 512,1,1,
    514,1,1, 516,1,1, 518,1,1, 520,1,1, 522,1,1, 524,1,1, 526,1,1, 528,1,1, 530,1,1,
    532,1,1, 534,1,1, 536,1,1, 538,1,1, 540,1,1, 542,1,1, 544,1,-130, 546,1,1, 548,1,1,
    550,1,1, 552,1,1, 554,1,1, 556,1,1, 558,1,1, 560,1,1, 562,1,1, 570,1,10795, 571,1,1,
    573,1,-163, 574,1,10792, 577,1,1, 579,1,-195, 580,1,69, 581,1,71, 582,1,1, 584,1,1,
    586,1,1, 588,1,1, 590,1,1, 880,1,1, 882,1,1, 886,1,1, 895,1,116, 902,1,38, 904,3,37,
    908,1,64, 910,2,63, 913,17,32, 931,9,32, 975,1,8, 984,1,1, 986,1,1, 988,1,1, 990,1,1,
    992,1,1, 994,1,1, 996,1,1, 998,1,1, 1000,1,1, 1002,1,1, 1004,1,1, 1006,1,1, 1012,1,-60,
    1015,1,1, 1017,1,-7, 1018,1,1, 1021,3,-130, 1024,16,80, 1040,32,32, 1120,1,1, 1122,1,1,
    1124,1,1, 1126,1,1, 1128,1,1, 1130,1,1, 1132,1,1, 1134,1,1, 1136,1,1, 1138,1,1,
    1140,1,1, 1142,1,1, 1144,1,1, 1146,1,1, 1148,1,1, 1150,1,1, 1152,1,1, 1162,1,1,
    1164,1,1, 1166,1,1, 1168,1,1, 1170,1,1, 1172,1,1, 1174,1,1, 1176,1,1, 1178,1,1,
    1180,1,1, 1182,1,1, 1184,1,1, 1186,1,1, 1188,1,1, 1190,1,1, 1192,1,1, 1194,1,1,
    1196,1,1, 1198,1,1, 1200,1,1, 1202,1,1, 1204,1,1, 1206,1,1, 1208,1,1, 1210,1,1,
    1212,1,1, 1214,1,1, 1216,1,15, 1217,1,1, 1219,1,1, 1221,1,1, 1223,1,1, 1225,1,1,
    1227,1,1, 1229,1,1, 1232,1,1, 1234,1,1, 1236,1,1, 1238,1,1, 1240,1,1, 1242,1,1,
    1244,1,1, 1246,1,1, 1248,1,1, 1250,1,1, 1252,1,1, 1254,1,1, 1256,1,1, 1258,1,1,
    1260,1,1, 1262,1,1, 1264,1,1, 1266,1,1, 1268,1,1, 1270,1,1, 1272,1,1, 1274,1,1,
    1276,1,1, 1278,1,1, 1280,1,1, 1282,1,1, 1284,1,1, 1286,1,1, 1288,1,1, 1290,1,1,
    1292,1,1, 1294,1,1, 1296,1,1, 1298,1,1, 1300,1,1, 1302,1,1, 1304,1,1, 1306,1,1,
    1308,1,1, 1310,1,1, 1312,1,1, 1314,1,1, 1316,1,1, 1318,1,1, 1320,1,1, 1322,1,1,
    1324,1,1, 1326,1,1, 1329,38,48, 4256,38,7264, 4295,1,7264, 4301,1,7264, 5024,80,38864,
    5104,6,8, 7312,43,-3008, 7357,3,-3008, 7680,1,1, 7682,1,1, 7684,1,1, 7686,1,1, 7688,1,1,
    7690,1,1, 7692,1,1, 7694,1,1, 7696,1,1, 7698,1,1, 7700,1,1, 7702,1,1, 7704,1,1,
    7706,1,1, 7708,1,1, 7710,1,1, 7712,1,1, 7714,1,1, 7716,1,1, 7718,1,1, 7720,1,1,
    7722,1,1, 7724,1,1, 7726,1,1, 7728,1,1, 7730,1,1, 7732,1,1, 7734,1,1, 7736,1,1,
    7738,1,1, 7740,1,1, 7742,1,1, 7744,1,1, 7746,1,1, 7748,1,1, 7750,1,1, 7752,1,1,
    7754,1,1, 7756,1,1, 7758,1,1, 7760,1,1, 7762,1,1, 7764,1,1, 7766,1,1, 7768,1,1,
    7770,1,1, 7772,1,1, 7774,1,1, 7776,1,1, 7778,1,1, 7780,1,1, 7782,1,1, 7784,1,1,
    7786,1,1, 7788,1,1, 7790,1,1, 7792,1,1, 7794,1,1, 7796,1,1, 7798,1,1, 7800,1,1,
    7802,1,1, 7804,1,1, 7806,1,1, 7808,1,1, 7810,1,1, 7812,1,1, 7814,1,1, 7816,1,1,
    7818,1,1, 7820,1,1, 7822,1,1, 7824,1,1, 7826,1,1, 7828,1,1, 7838,1,-7615, 7840,1,1,
    7842,1,1, 7844,1,1, 7846,1,1, 7848,1,1, 7850,1,1, 7852,1,1, 7854,1,1, 7856,1,1,
    7858,1,1, 7860,1,1, 7862,1,1, 7864,1,1, 7866,1,1, 7868,1,1, 7870,1,1, 7872,1,1,
    7874,1,1, 7876,1,1, 7878,1,1, 7880,1,1, 7882,1,1, 7884,1,1, 7886,1,1, 7888,1,1,
    7890,1,1, 7892,1,1, 7894,1,1, 7896,1,1, 7898,1,1, 7900,1,1, 7902,1,1, 7904,1,1,
    7906,1,1, 7908,1,1, 7910,1,1, 7912,1,1, 7914,1,1, 7916,1,1, 7918,1,1, 7920,1,1,
    7922,1,1, 7924,1,1, 7926,1,1, 7928,1,1, 7930,1,1, 7932,1,1, 7934,1,1, 7944,8,-8,
    7960,6,-8, 7976,8,-8, 7992,8,-8, 8008,6,-8, 8025,1,-8, 8027,1,-8, 8029,1,-8, 8031,1,-8,
    8040,8,-8, 8072,8,-8, 8088,8,-8, 8104,8,-8, 8120,2,-8, 8122,2,-74, 8124,1,-9,
    8136,4,-86, 8140,1,-9, 8152,2,-8, 8154,2,-100, 8168,2,-8, 8170,2,-112, 8172,1,-7,
    8184,2,-128, 8186,2,-126, 8188,1,-9, 8486,1,-7517, 8490,1,-8383, 8491,1,-8262,
    8498,1,28, 8544,16,16, 8579,1,1, 9398,26,26, 11264,48,48, 11360,1,1, 11362,1,-10743,
    11363,1,-3814, 11364,1,-10727, 11367,1,1, 11369,1,1, 11371,1,1, 11373,1,-10780,
    11374,1,-10749, 11375,1,-10783, 11376,1,-10782, 11378,1,1, 11381,1,1, 11390,2,-10815,
    11392,1,1, 11394,1,1, 11396,1,1, 11398,1,1, 11400,1,1, 11402,1,1, 11404,1,1, 11406,1,1,
    11408,1,1, 11410,1,1, 11412,1,1, 11414,1,1, 11416,1,1, 11418,1,1, 11420,1,1, 11422,1,1,
    11424,1,1, 11426,1,1, 11428,1,1, 11430,1,1, 11432,1,1, 11434,1,1, 11436,1,1, 11438,1,1,
    11440,1,1, 11442,1,1, 11444,1,1, 11446,1,1, 11448,1,1, 11450,1,1, 11452,1,1, 11454,1,1,
    11456,1,1, 11458,1,1, 11460,1,1, 11462,1,1, 11464,1,1, 11466,1,1, 11468,1,1, 11470,1,1,
    11472,1,1, 11474,1,1, 11476,1,1, 11478,1,1, 11480,1,1, 11482,1,1, 11484,1,1, 11486,1,1,
    11488,1,1, 11490,1,1, 11499,1,1, 11501,1,1, 11506,1,1, 42560,1,1, 42562,1,1, 42564,1,1,
    42566,1,1, 42568,1,1, 42570,1,1, 42572,1,1, 42574,1,1, 42576,1,1, 42578,1,1, 42580,1,1,
    42582,1,1, 42584,1,1, 42586,1,1, 42588,1,1, 42590,1,1, 42592,1,1, 42594,1,1, 42596,1,1,
    42598,1,1, 42600,1,1, 42602,1,1, 42604,1,1, 42624,1,1, 42626,1,1, 42628,1,1, 42630,1,1,
    42632,1,1, 42634,1,1, 42636,1,1, 42638,1,1, 42640,1,1, 42642,1,1, 42644,1,1, 42646,1,1,
    42648,1,1, 42650,1,1, 42786,1,1, 42788,1,1, 42790,1,1, 42792,1,1, 42794,1,1, 42796,1,1,
    42798,1,1, 42802,1,1, 42804,1,1, 42806,1,1, 42808,1,1, 42810,1,1, 42812,1,1, 42814,1,1,
    42816,1,1, 42818,1,1, 42820,1,1, 42822,1,1, 42824,1,1, 42826,1,1, 42828,1,1, 42830,1,1,
    42832,1,1, 42834,1,1, 42836,1,1, 42838,1,1, 42840,1,1, 42842,1,1, 42844,1,1, 42846,1,1,
    42848,1,1, 42850,1,1, 42852,1,1, 42854,1,1, 42856,1,1, 42858,1,1, 42860,1,1, 42862,1,1,
    42873,1,1, 42875,1,1, 42877,1,-35332, 42878,1,1, 42880,1,1, 42882,1,1, 42884,1,1,
    42886,1,1, 42891,1,1, 42893,1,-42280, 42896,1,1, 42898,1,1, 42902,1,1, 42904,1,1,
    42906,1,1, 42908,1,1, 42910,1,1, 42912,1,1, 42914,1,1, 42916,1,1, 42918,1,1, 42920,1,1,
    42922,1,-42308, 42923,1,-42319, 42924,1,-42315, 42925,1,-42305, 42926,1,-42308,
    42928,1,-42258, 42929,1,-42282, 42930,1,-42261, 42931,1,928, 42932,1,1, 42934,1,1,
    42936,1,1, 42938,1,1, 42940,1,1, 42942,1,1, 42944,1,1, 42946,1,1, 42948,1,-48,
    42949,1,-42307, 42950,1,-35384, 42951,1,1, 42953,1,1, 42960,1,1, 42966,1,1, 42968,1,1,
    42997,1,1, 65313,26,32, 66560,40,40, 66736,36,40, 66928,11,39, 66940,15,39, 66956,7,39,
    66964,2,39, 68736,51,64, 71840,32,32, 93760,32,32, 125184,34,34
  ];
  /* END GENERATED FOLD TABLE */

  /* BEGIN GENERATED SPACE TABLE */
  var SPACE_TABLE = [
    9,5, 32,1, 133,1, 160,1, 5760,1, 8192,11, 8232,2, 8239,1, 8287,1, 12288,1
  ];
  /* END GENERATED SPACE TABLE */

  /* foldCodePoint folds ONE code point through the table, by binary search over
   * the runs. The search is what the table's ordering and disjointness are for:
   * at most one run can contain a code point, and the halving finds it in about
   * ten steps out of several hundred runs. A code point no run covers is its own
   * fold, which is the mapping's default and needs no entry. */
  function foldCodePoint(cp) {
    var lo = 0;
    var hi = FOLD_TABLE.length / 3 - 1;
    while (lo <= hi) {
      var mid = (lo + hi) >> 1;
      var at = 3 * mid;
      if (cp < FOLD_TABLE[at]) {
        hi = mid - 1;
      } else if (cp >= FOLD_TABLE[at] + FOLD_TABLE[at + 1]) {
        lo = mid + 1;
      } else {
        return cp + FOLD_TABLE[at + 2];
      }
    }
    return cp;
  }

  /* isSpaceCodePoint answers whether ONE code point carries the White_Space
   * property, by binary search over the spans, exactly as foldCodePoint searches
   * the runs. A code point no span covers is not whitespace, which is the set's
   * default and needs no entry. */
  function isSpaceCodePoint(cp) {
    var lo = 0;
    var hi = SPACE_TABLE.length / 2 - 1;
    while (lo <= hi) {
      var mid = (lo + hi) >> 1;
      var at = 2 * mid;
      if (cp < SPACE_TABLE[at]) {
        hi = mid - 1;
      } else if (cp >= SPACE_TABLE[at] + SPACE_TABLE[at + 1]) {
        lo = mid + 1;
      } else {
        return true;
      }
    }
    return false;
  }

  /* trimTerm removes the whitespace at the term's two ends by the SERVER'S set,
   * and calls no trimming function of the JavaScript platform: the platform's
   * removes a different set, so the same term would lose different code points
   * here than on the server (see the note at the top of this file).
   *
   * Removal stops at the first code point that does not carry the property, so
   * whitespace INSIDE the term survives and is matched literally. A term made
   * only of such code points becomes "", which is no term at all.
   *
   * Both walks are by CODE POINT, never by UTF-16 code unit. Walking forwards is
   * codePointAt and a step of one unit or two; walking backwards has to look at
   * the unit before the low surrogate to find where the code point began, which
   * is what the pairing test does. No whitespace code point is astral today, but
   * a walk that split a surrogate pair would ask the table about a lone surrogate
   * half rather than about the character the user typed. */
  function trimTerm(raw) {
    var start = 0;
    var end = raw.length;
    while (start < end) {
      var head = raw.codePointAt(start);
      if (!isSpaceCodePoint(head)) {
        break;
      }
      start += head > 0xffff ? 2 : 1;
    }
    while (end > start) {
      var at = end - 1;
      var last = raw.charCodeAt(at);
      if (last >= 0xdc00 && last <= 0xdfff && at - 1 >= start) {
        var first = raw.charCodeAt(at - 1);
        if (first >= 0xd800 && first <= 0xdbff) {
          at -= 1;
        }
      }
      if (!isSpaceCodePoint(raw.codePointAt(at))) {
        break;
      }
      end = at;
    }
    return raw.slice(start, end);
  }

  /* foldTerm normalises what the user typed into the form the matching rule
   * compares with, mirroring the server's foldSearchTerm: the term's ends
   * stripped by trimTerm, THEN every code point folded through FOLD_TABLE. A term
   * that is empty or all whitespace normalises to "", which is no term at all and
   * shows every task.
   *
   * The order is the server's, and it is fixed rather than left to chance: no
   * code point carrying White_Space folds to anything but itself and none outside
   * the property folds into it, so the two steps commute today — but both paths
   * take them in the same order anyway, so the contract does not rest on that.
   *
   * The fold's walk is by CODE POINT, never by UTF-16 code unit: codePointAt
   * returns the whole astral code point and the index then advances by the two
   * units it occupies, so a surrogate pair is folded as the one character it is
   * instead of as two halves that no run covers. */
  function foldTerm(raw) {
    var trimmed = trimTerm(raw);
    var folded = "";
    for (var i = 0; i < trimmed.length; ) {
      var cp = trimmed.codePointAt(i);
      folded += String.fromCodePoint(foldCodePoint(cp));
      i += cp > 0xffff ? 2 : 1;
    }
    return folded;
  }

  /* controls reads the four header controls into the one shape the rest of this
   * file works with: the raw term (echoed back as text), the folded term, and one
   * value per filter, "" meaning that dimension carries no filter — the same
   * meaning the parameter's absence has in the URL and the zero value has on the
   * server. */
  function controls() {
    var values = [];
    for (var i = 0; i < filters.length; i++) {
      values.push(filters[i].control ? filters[i].control.value : "");
    }
    return { raw: input.value, term: foldTerm(input.value), filters: values };
  }

  /* active reports whether any control is narrowing the board, which is what
   * separates a board narrowed to nothing — which says so — from a roadmap that
   * holds no task at all, which does not. */
  function active(state) {
    if (state.term !== "") {
      return true;
    }
    for (var i = 0; i < state.filters.length; i++) {
      if (state.filters[i] !== "") {
        return true;
      }
    }
    return false;
  }

  /* matchesTerm applies the search criterion: the term occurs, as a substring, in
   * the task's title or in its "#<id>" reference. The title arrives already
   * folded from the server; the reference is digits and "#", which no case
   * folding changes. No other field is searched. */
  function matchesTerm(card, term) {
    if (term === "") {
      return true;
    }
    var title = card.getAttribute("data-search") || "";
    if (title.indexOf(term) !== -1) {
      return true;
    }
    var reference = "#" + (card.getAttribute("data-task-id") || "");
    return reference.indexOf(term) !== -1;
  }

  /* matches is the board's ONE verdict on one card: shown when it satisfies EVERY
   * active criterion. An inactive filter is skipped rather than compared, which
   * is what makes a board with no active control show every task. */
  function matches(card, state) {
    if (!matchesTerm(card, state.term)) {
      return false;
    }
    for (var i = 0; i < filters.length; i++) {
      var value = state.filters[i];
      if (value === "") {
        continue;
      }
      if (!filters[i].compare(card.getAttribute(filters[i].attribute) || "", value)) {
        return false;
      }
    }
    return true;
  }

  /* apply narrows the board to the current controls and brings everything the
   * board says about itself back into agreement with what it is showing: the
   * cards, the per-column counts, the per-column empty states, and the board's own
   * no-match message. */
  function apply(state) {
    var shown = 0;

    for (var c = 0; c < columns.length; c++) {
      var column = columns[c];
      var cards = column.querySelectorAll(".task-card");
      var visible = 0;

      for (var i = 0; i < cards.length; i++) {
        var show = matches(cards[i], state);
        cards[i].hidden = !show;
        if (show) {
          visible++;
        }
      }

      // The count states what the column is showing, never the unnarrowed total.
      var badge = column.querySelector(".card-title .badge");
      if (badge) {
        badge.textContent = String(visible);
      }
      // A column emptied by the controls reads exactly like a column the roadmap
      // left empty.
      var columnEmpty = column.querySelector('[data-role="task-board-column-empty"]');
      if (columnEmpty) {
        columnEmpty.hidden = visible > 0;
      }
      shown += visible;
    }

    // The board's own message, shown only when a control is in force and nothing
    // matched the conjunction of all of them. ONE message covers the term and the
    // filters together; the phrase naming the term is shown only when there is a
    // term, and the term itself is written as TEXT, never as markup.
    if (boardEmpty) {
      if (boardEmptyTerm) {
        boardEmptyTerm.textContent = state.raw;
      }
      if (boardEmptyTermPhrase) {
        boardEmptyTermPhrase.hidden = state.term === "";
      }
      boardEmpty.hidden = !(active(state) && shown === 0);
    }
  }

  /* syncURL keeps the address bar showing the board on screen, so a narrowed view
   * is shareable and reloadable. The current history entry is REPLACED rather
   * than a new one pushed: one entry per keystroke, or one per dropdown change,
   * would turn the Back button into an undo key for narrowing. A control on its
   * no-filter option — an empty term, a dropdown on "Any ..." — REMOVES its
   * parameter rather than leaving an empty one behind, so clearing every control
   * leaves the bare page URL. */
  function syncURL(state) {
    if (!window.history || !window.history.replaceState) {
      return;
    }
    var url = new URL(window.location.href);
    if (state.term === "") {
      url.searchParams.delete("q");
    } else {
      url.searchParams.set("q", state.raw);
    }
    for (var i = 0; i < filters.length; i++) {
      if (state.filters[i] === "") {
        url.searchParams.delete(filters[i].param);
      } else {
        url.searchParams.set(filters[i].param, state.filters[i]);
      }
    }
    window.history.replaceState(window.history.state, "", url.toString());
  }

  /* narrow is the single entry point every control ends in, so the four controls
   * cannot drift into four behaviours. */
  function narrow() {
    var state = controls();
    apply(state);
    syncURL(state);
  }

  input.addEventListener("input", narrow);

  /* A search input offers a native clear control, which fires "search" rather
   * than "input" in some browsers; both paths end in the same call. */
  input.addEventListener("search", narrow);

  for (var f = 0; f < filters.length; f++) {
    if (filters[f].control) {
      filters[f].control.addEventListener("change", narrow);
    }
  }

  /* The board arrives already narrowed by the server when the URL carried any of
   * the four values, so nothing is applied on load: re-applying would repeat work
   * the server did and would be the post-load narrowing the specification rules
   * out. */
})();

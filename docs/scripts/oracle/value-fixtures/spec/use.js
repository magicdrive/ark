const m1 = require("./m1");
const m2 = require("./m2");
const m3 = require("./m3");
const m5 = require("./m5");
const m6 = require("./m6");
const m7 = require("./m7");
const m8 = require("./m8");
const m9 = require("./m9");
const m10 = require("./m10");
const m11 = require("./m11");
const m15 = require("./m15");
const m16 = require("./m16");
const { parse: d1 } = require("./m1");
function u1() { return m1.parse(); }
function u1d() { return d1; }
function u2() { return m2.parse(); }
function u3() { return m3.execute(); }
function u5() { return m5.parse(); }
function u6() { return m6.parse(); }
function u7() { return m7.parse(); }
function u8() { return m8.parse; }
function u9() { return m9.tools.parse(); }
function u10() { return m10.parse(); }
function u11() { return m11.parse(); }
function u15() { return m15.parse(); }
function u16() { return m16.parse(); }
module.exports = { u1, u1d, u2, u3, u5, u6, u7, u8, u9, u10, u11, u15, u16 };

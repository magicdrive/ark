function a() {}
function b() {}
module.exports = { a, X: 1 }
const reg = (x) => x
reg([module.exports])

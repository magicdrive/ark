<?php
class Helper {
    public static function go(): void {}
}
class Service {
    public function run(): void {
        Helper::go();
    }
}
class Unrelated1 { public function x1(): void {} }
class Unrelated2 { public function x2(): void {} }
class Unrelated3 { public function x3(): void {} }
function unrelatedUtilA(): void {}
function unrelatedUtilB(): void {}
function unrelatedUtilC(): void {}

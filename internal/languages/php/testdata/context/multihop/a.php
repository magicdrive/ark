<?php
class Repo {
    public function find(): void {}
}
class Service {
    public function run(): void {
        $r = new Repo();
        $r->find();
    }
}
class Controller {
    public function handle(): void {
        $s = new Service();
        $s->run();
    }
}

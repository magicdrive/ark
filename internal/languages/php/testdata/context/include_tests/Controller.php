<?php
class Controller {
    public function handle(Service $s): void {
        $s->run();
    }
}

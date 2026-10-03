<?php
class Service {
    public function a(): void {
        $this->b();
    }
    public function b(): void {}
}

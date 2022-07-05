(module
  (global $g (mut i32) (i32.const 1))
  (func (export "entry") (param $x i32) (result i32)
    i32.const 666
    global.set $g
    nop
    global.get $g
  )
)

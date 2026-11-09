*This tool was written mostly by Claude 5.5 but it works well enough.*

## img2stl turns a two-color image into a 3D-printable STL file.

- white pixels -> base height
- black pixels -> raised by -raise millimetres 


No external libraries are needed. 
PNG, JPEG and GIF inputs are accepted.
Works ideally with a purely black and white image, colored outputs might not work as expected.

The result is a single watertight solid, a flat slab `-base` mm thick with black areas raised by `-raise` mm.

`-base 0` removes the slab, the model becomes just the black areas as a `-raise` mm thick layer.

`-setback n` shapes the white slab into an outline of all the black areas, set back by `n` mm. When `n` is 0, it only fills in inner areas, with no outline.

```sh
# simply convert input.png into input.stl
img2stl input.png

# with custom scaling and output filename
img2stl -px 0.2 -base 1.5 -raise 0.8 -o output.stl input.png

# setback mode
img2stl -setback 2 input.png
```
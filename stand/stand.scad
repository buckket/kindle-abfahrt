include <BOSL/constants.scad>
use <BOSL/shapes.scad>
use <BOSL/transforms.scad>

difference() {
cuboid([140,70,50], fillet=4, edges=EDGES_ALL-EDGES_BOTTOM);
move(x=0,y=-10,z=-20) ycyl(l=80, r=30);
move(x=0,y=7,z=25) xrot(20) cuboid([118,11,50], center=true);
}
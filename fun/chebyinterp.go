// Copyright 2016 The Gosl Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fun

import (
	"math"

	"github.com/cpmech/gosl/chk"
	"github.com/cpmech/gosl/la"
)

// ChebyInterp defines a structure for efficient computations with Chebyshev polynomials such as
// projecttion or interpolation
//
//   Some equations are based on [1]
//
//   Reference:
//     [1] Canuto C, Hussaini MY, Quarteroni A, Zang TA (2006) Spectral Methods: Fundamentals in
//         Single Domains. Springer. 563p
//
type ChebyInterp struct {

	// input
	N     int  // degree of polynomial
	Gauss bool // use roots (Gauss) or points (Lobatto)?

	// derived
	X      []float64 // points. NOTE: this mirrowed version of Chebyshev X; i.e. from +1 to -1
	Wb     []float64 // weights for Gaussian quadrature
	Gamma  []float64 // denominador of coefficients equation ~ ‖p[i]‖²
	CoefI  []float64 // coefficients of interpolant
	CoefP  []float64 // coefficients of projection (estimated)
	CoefIs []float64 // coefficients of interpolation using Lagrange cardinal functions

	// constants
	EstimationN int // N to use when estimating CoefP [default=128]

	// computed
	C  *la.Matrix // physical to transform space conversion matrix
	Ci *la.Matrix // transform to physical space conversion matrix
	D1 *la.Matrix // (dψj/dx)(xi)
	D2 *la.Matrix // (d²ψj/dx²)(xi)
}

// NewChebyInterp returns a new ChebyInterp structure
//
//   gaussChebyshev == true:
//
//                     /  (2⋅j+1)⋅π  \
//           X_j = cos | ——————————— |       j = 0 ... N
//                     \   2⋅N + 2   /
//
//   gaussChebyshev == false: (Gauss-Lobatto-Chebyshev)
//
//                     /  j⋅π  \
//           X_j = cos | ————— |       j = 0 ... N
//                     \   N   /
//
//   NOTE: X here is the mirrowed version of Chebyshev X; i.e. from +1 to -1
//
func NewChebyInterp(N int, gaussChebyshev bool) (o *ChebyInterp, err error) {

	// allocate
	o = new(ChebyInterp)
	o.N = N
	o.Gauss = gaussChebyshev
	o.Wb = make([]float64, N+1)
	o.Gamma = make([]float64, N+1)
	o.CoefI = make([]float64, N+1)
	o.CoefP = make([]float64, N+1)
	o.EstimationN = 128

	// roots or points
	var xx []float64
	if gaussChebyshev {
		xx = ChebyshevXgauss(N)
	} else {
		xx = ChebyshevXlob(N)
	}
	o.X = make([]float64, o.N+1)
	for i := 0; i < o.N+1; i++ {
		o.X[i] = xx[o.N-i]
	}

	// set data
	wb, wb0, wbN, gam, gam0, gamN := o.gaussData(o.N)
	for i := 0; i < o.N+1; i++ {
		o.Wb[i] = wb
		o.Gamma[i] = gam
	}
	o.Wb[0] = wb0
	o.Wb[o.N] = wbN
	o.Gamma[0] = gam0
	o.Gamma[o.N] = gamN
	return
}

// CalcConvMats computes conversion matrices C and Ci
//
//                  N
//    trans(u)_k =  Σ  C_{kj} ⋅ u(x_j)      e.g. trans(u)_k == coefI_k
//                 j=0
//
//           N
//    u_j =  Σ   C⁻¹_{jk} ⋅ trans(u)_k
//          k=0
//
//                   2             /  j⋅k⋅π  \
//    C_{kj} = ————————————— ⋅ cos | ——————— |
//              cb_k⋅cb_j⋅N        \    N    /
//
//                   /  j⋅k⋅π  \
//    C⁻¹_{jk} = cos | ——————— |
//                   \    N    /
//
func (o *ChebyInterp) CalcConvMats() {
	o.C = la.NewMatrix(o.N+1, o.N+1)
	o.Ci = la.NewMatrix(o.N+1, o.N+1)
	n := float64(o.N)
	for k := 0; k < o.N+1; k++ {
		cbk := o.cbar(k)
		for j := 0; j < o.N+1; j++ {
			cbj := o.cbar(j)
			v := math.Cos(π * float64(k*j) / n)
			o.C.Set(k, j, 2.0*v/(cbk*cbj*n))
			o.Ci.Set(j, k, v)
		}
	}
}

// CalcCoefI computes the coefficients of the interpolant by (slow) direct formula
//
//              1    N
//   CoefI_k = ——— ⋅ Σ  f(x_j) ⋅ T_k(x_j) ⋅ wb_j
//             γ_k  j=0
//
//   Thus (for Gauss-Lobatto):
//
//               2       N    1                   /  k⋅j⋅π  \
//   CoefI_k = —————— ⋅  Σ  —————— ⋅ f(x_j) ⋅ cos | ——————— |
//             N⋅cb_k   j=0  cb_j                 \    N    /
//
//   where:
//           cb_k = 2 if j=0,N   or   1 if j=1...N-1
//
//   NOTE: the results will be stored in o.CoefI
//
func (o *ChebyInterp) CalcCoefI(f Ss) (err error) {

	// evaluate function at all points
	fx := make([]float64, o.N+1)
	for i := 0; i < o.N+1; i++ {
		fx[i], err = f(o.X[i])
		if err != nil {
			return
		}
	}

	// computation of coefficients
	for k := 0; k < o.N+1; k++ {
		o.CoefI[k] = 0
		for j := 0; j < o.N+1; j++ {
			o.CoefI[k] += fx[j] * ChebyshevT(k, o.X[j]) * o.Wb[j]
		}
		o.CoefI[k] /= o.Gamma[k]
	}
	return
}

// CalcCoefP computes the coefficients of the projection (slow)
// using o.EstimationN + 1 points
//
//               ∫ f(x)⋅T_k(x)⋅w(x) dx      (f, T_k)_w
//   CoefP_k = ————————————————————————— = ————————————
//              ∫ T_k(x)⋅T_k(x)⋅w(x) dx      ‖ T_k ‖²
//
//   NOTE: the results will be stored in o.CoefP
//
func (o *ChebyInterp) CalcCoefP(f Ss) (err error) {

	// quadrature data
	nn := o.EstimationN
	wb, wb0, wbN, gam, gam0, gamN := o.gaussData(nn)

	// evaluate function at many points
	var xx []float64
	if o.Gauss {
		xx = ChebyshevXgauss(nn)
	} else {
		xx = ChebyshevXlob(nn)
	}
	fx := make([]float64, nn+1)
	for i := 0; i < nn+1; i++ {
		fx[i], err = f(xx[i])
		if err != nil {
			return
		}
	}

	// computation of coefficients using quadrature
	for i := 0; i < o.N+1; i++ {
		o.CoefP[i] = 0
		for j := 1; j < nn; j++ {
			o.CoefP[i] += fx[j] * ChebyshevT(i, xx[j]) * wb
		}
		o.CoefP[i] += fx[0] * ChebyshevT(i, xx[0]) * wb0
		o.CoefP[i] += fx[nn] * ChebyshevT(i, xx[nn]) * wbN
		if i == 0 {
			o.CoefP[i] /= gam0
		} else if i == nn {
			o.CoefP[i] /= gamN
		} else {
			o.CoefP[i] /= gam
		}
	}
	return
}

// I computes the interpolation
//
//               N
//    I{f}(x) =  Σ  CoefI_k ⋅ T_k(x)
//              k=0
//
//    Thus:
//
//              N
//    f(x_j) =  Σ   CoefI_k ⋅ T_k(x_j)
//             k=0
//
//    NOTE: CoefI coefficients must be computed first
//
func (o *ChebyInterp) I(x float64) (res float64) {
	for k := 0; k < o.N+1; k++ {
		res += o.CoefI[k] * ChebyshevT(k, x)
	}
	return
}

// P computes the (approximated) projection
//
//               ∞
//    S{f}(x) =  Σ  CoefP_k ⋅ T_k(x)   (series representation)
//              k=0
//
//    Thus:
//
//               N
//    P{f}(x) =  Σ  CoefP_k ⋅ T_k(x)   (truncated series)
//              k=0
//
//    NOTE: CoefP coefficients must be computed first
//
func (o *ChebyInterp) P(x float64) (res float64) {
	for k := 0; k < o.N+1; k++ {
		res += o.CoefP[k] * ChebyshevT(k, x)
	}
	return
}

// EstimateMaxErr estimates the maximum error using 10000 stations along [-1,1]
// This function also returns the location (xloc) of the estimated max error
//
//    maxerr = max(|f - I{f}|)  or  maxerr = max(|f - P{f}|)
//
//    NOTE: CoefI or CoefP must be computed first
//
func (o *ChebyInterp) EstimateMaxErr(f Ss, projection bool) (maxerr, xloc float64) {
	nsta := 10000 // generate several points along [-1,1]
	xloc = -1
	var fa float64
	for i := 0; i < nsta; i++ {
		x := -1.0 + 2.0*float64(i)/float64(nsta-1)
		fx, err := f(x)
		if err != nil {
			chk.Panic("f(x) failed:%v\n", err)
		}
		if projection {
			fa = o.P(x)
		} else {
			fa = o.I(x)
		}
		e := math.Abs(fx - fa)
		if e > maxerr {
			maxerr = e
			xloc = x
		}
	}
	return
}

// HierarchicalT computes Tn(x) using hierarchical definition (but NOT recursive)
//   NOTE: this function is not as efficient as ChebyshevT and should be used for testing only
func (o *ChebyInterp) HierarchicalT(i int, x float64) float64 {
	if i == 0 {
		return 1.0
	}
	if i == 1 {
		return x
	}
	tjm2 := 1.0 // value at step j - 2
	tjm1 := x   // value at step j - 1
	var tj float64
	for j := 2; j <= i; j++ {
		tj = 2*x*tjm1 - tjm2
		tjm2 = tjm1
		tjm1 = tj
	}
	return tjm1
}

// CalcCoefIs computes the coefficients for interpolation with Lagrange cardinal functions ψ_l(x)
func (o *ChebyInterp) CalcCoefIs(f Ss) (err error) {
	if len(o.CoefIs) != o.N+1 {
		o.CoefIs = make([]float64, o.N+1)
	}
	for l := 0; l < o.N+1; l++ {
		o.CoefIs[l], err = f(o.X[l])
		if err != nil {
			return
		}
	}
	return
}

// Is computes the interpolation using the Lagrange cardinal functions ψ_l(x)
//
//              N                                         N
//   I{f}(x) =  Σ   f(x_l) ⋅ ψ_l(x)    or      I{f}(x) =  Σ  CoefIs_l ⋅ ψ_l(x)
//             l=0                                       l=0
//
//   NOTE: CoefIs == f(x_l) coefficients must be computed first
//
func (o *ChebyInterp) Is(x float64) (res float64) {
	for l := 0; l < o.N+1; l++ {
		res += o.CoefIs[l] * o.PsiLobDirect(l, x)
	}
	return
}

// PsiLobDirect evaluates the Lagrange cardinal function ψ_l(x) of degree N with Gauss-Lobatto points
//
//              N
//   I{f}(x) =  Σ   f(x_l) ⋅ ψ_l(x)
//             l=0
//
//   Equation (2.4.30), page 88 of [1]
//
//   NOTE: must not use with Gauss (roots) points
//
func (o *ChebyInterp) PsiLobDirect(l int, x float64) float64 {
	if math.Abs(x-o.X[l]) < 1e-14 {
		return 1
	}
	nn := float64(o.N * o.N)
	cbl := o.cbar(l)
	dTn := ChebyshevTdiff1(o.N, x)
	return NegOnePowN(l+1) * (1.0 - x*x) * dTn / (cbl * nn * (x - o.X[l]))
}

// CalcD1 computes the differentiation matrix D1 of the function PsiLobDirect
//
//    d I{f}(x)     N            d ψ_l(x)
//   ——————————— =  Σ   f(x_l) ⋅ ————————
//        dx       l=0              dx
//
//    d I{f}(x)  |         N
//   ——————————— |      =  Σ   D1_jl ⋅ f(x_l)
//        dx     |x=x_j   l=0
//
//   where:
//
//            dψ_l  |
//    D1_jl = ————— |
//             dx   |x=x_j
//
//   INPUT:
//     trigo -- [best] use trigonometric identities (to reduce round-off errors)
//     flip  -- [best] compute lower-diagonal part from upper diagonal part with:
//                              D_{N-j,N-l} = -D_{j,l}
//
//   NOTE: (1) the signs are swapped (compared to [1]) because X are reversed here (from -1 to +1)
//         (2) this method is only available for Gauss-Lobatto points
//
//   Equations (2.4.31) and (2.4.33), page 89 of [1]
//
func (o *ChebyInterp) CalcD1(trigo, flip bool) (err error) {

	// check
	if o.Gauss {
		return chk.Err("cannot compute D1 for non-Gauss-Lobatto points\n")
	}

	// allocate output and declare some constants/variables
	o.D1 = la.NewMatrix(o.N+1, o.N+1)
	n := float64(o.N)
	nn := float64(o.N * o.N)
	n2 := 2.0 * n
	var v, s1, s2, jj, ll, cbj, cbl float64

	// using trigonometric identities
	if trigo {
		for j := 0; j < o.N+1; j++ {
			lMin := 0
			if flip {
				lMin = j
			}
			for l := lMin; l < o.N+1; l++ {
				if j == l {
					if j == 0 {
						v = (2.0*nn + 1.0) / 6.0
					} else if j == o.N {
						v = -(2.0*nn + 1.0) / 6.0
					} else {
						jj = float64(j)
						s1 = math.Sin(jj * π / n)
						v = -o.X[j] / (2.0 * s1 * s1)
					}
				} else {
					jj = float64(j)
					ll = float64(l)
					cbj = o.cbar(j)
					cbl = o.cbar(l)
					s1 = math.Sin((jj + ll) * π / n2)
					s2 = math.Sin((jj - ll) * π / n2)
					v = -cbj * NegOnePowN(j+l) / (2.0 * cbl * s1 * s2)
				}
				o.D1.Set(j, l, v)
			}
		}
		if flip {
			for j := 0; j < o.N+1; j++ {
				for l := j + 1; l < o.N+1; l++ {
					o.D1.Set(o.N-j, o.N-l, -o.D1.Get(j, l))
				}
			}
		}
		return
	}

	// direct derivatives
	for j := 0; j < o.N+1; j++ {
		lMin := 0
		if flip {
			lMin = j
		}
		for l := lMin; l < o.N+1; l++ {
			if j == l {
				if j == 0 {
					v = (2.0*nn + 1.0) / 6.0
				} else if j == o.N {
					v = -(2.0*nn + 1.0) / 6.0
				} else {
					v = -o.X[l] / (2.0 * (1.0 - o.X[l]*o.X[l]))
				}
			} else {
				cbj = o.cbar(j)
				cbl = o.cbar(l)
				v = cbj * NegOnePowN(j+l) / (cbl * (o.X[j] - o.X[l]))
			}
			o.D1.Set(j, l, v)
		}
	}
	if flip {
		for j := 0; j < o.N+1; j++ {
			for l := j + 1; l < o.N+1; l++ {
				o.D1.Set(o.N-j, o.N-l, -o.D1.Get(j, l))
			}
		}
	}
	return
}

// CalcD2 calculates the second derivative
//
//            d²ψ_l  |
//    D2_jl = —————— |
//             dx²   |x=x_j
//
//    Equation (2.4.32), page 89 of [1]
//
func (o *ChebyInterp) CalcD2() (err error) {

	// check
	if o.Gauss {
		return chk.Err("cannot compute D2 for non-Gauss-Lobatto points\n")
	}

	// allocate output and declare some constants/variables
	o.D2 = la.NewMatrix(o.N+1, o.N+1)
	nn := float64(o.N * o.N)
	nn2p1 := 2.0*nn + 1.0
	NN := nn * nn
	tt := 2.0 / 3.0
	var v, s, cbl, d float64

	// compute D2 matrix
	for j := 0; j < o.N+1; j++ {
		for l := 0; l < o.N+1; l++ {
			if j == l {
				if j == 0 || j == o.N {
					v = (NN - 1.0) / 15.0
				} else {
					s = 1.0 - o.X[j]*o.X[j]
					v = -((nn-1.0)*s + 3.0) / (3.0 * s * s)
				}
			} else {
				if j == 0 {
					cbl = o.cbar(l)
					d = (1.0 - o.X[l])
					v = (tt / cbl) * NegOnePowN(l) * (nn2p1*d - 6.0) / (d * d)
				} else if j == o.N {
					cbl = o.cbar(l)
					d = (1.0 + o.X[l])
					v = (tt / cbl) * NegOnePowN(l+o.N) * (nn2p1*d - 6.0) / (d * d)
				} else {
					cbl = o.cbar(l)
					s = 1.0 - o.X[j]*o.X[j]
					d = o.X[j] - o.X[l]
					v = (1.0 / cbl) * NegOnePowN(j+l) * (o.X[j]*o.X[j] + o.X[j]*o.X[l] - 2.0) / (s * d * d)
				}
			}
			o.D2.Set(j, l, v)
		}
	}
	return
}

// CalcErrorD1 computes the maximum error due to differentiation (@ X[i]) using the D1 matrix
//   NOTE: CoefIs and D1 matrix must be computed previously
func (o *ChebyInterp) CalcErrorD1(f, dfdxAna Ss) (maxDiff float64) {

	// f @ nodes: u = f(x_i)
	u := o.CoefIs

	// derivative of interpolation @ x_i
	v := la.NewVector(o.N + 1)
	la.MatVecMul(v, 1, o.D1, u)

	// compute error
	for i := 0; i < o.N+1; i++ {
		vana, err := dfdxAna(o.X[i])
		chk.EP(err)
		diff := math.Abs(v[i] - vana)
		if diff > maxDiff {
			maxDiff = diff
		}
	}
	return
}

// auxiliary //////////////////////////////////////////////////////////////////////////////////////

// cbar returns 2 if j=0,N or 1 if j=1,...,N-1
func (o *ChebyInterp) cbar(j int) float64 {
	if j == 0 || j == o.N {
		return 2
	}
	return 1
}

// gaussData returns quadrature data for either Gauss-Chebyshev or Gauss-Lobatto
func (o *ChebyInterp) gaussData(N int) (wb, wb0, wbN, gam, gam0, gamN float64) {

	// Gauss
	if o.Gauss {
		wb = π / float64(N+1)
		wb0 = wb
		wbN = wb
		gam = π / 2.0
		gam0 = π
		gamN = π / 2.0
		return
	}

	// Gauss-Lobatto
	wb = π / float64(N)
	wb0 = π / float64(2*N)
	wbN = wb0
	gam = π / 2.0
	gam0 = π
	gamN = π
	return
}
#!/usr/bin/env python3
"""固定(无随机)生成 TGA 失重谱系离线案例包的样例文件。
真实质量模型: m_true(t)=10.00 - 双曲正切损失脉冲 A..E
样品温度相对炉程序有 3 min 滞后; 原始质量示值含 -0.0020 mg/min 天平漂移。
"""
import math, os

OUT = os.path.dirname(os.path.abspath(__file__))

def furnace(t):
    if t <= 20: return 30 + 8.5*t
    if t <= 40: return 200 + 20*(t-20)
    if t <= 70: return 600 + 10*(t-40)
    return 900.0

def sample_temp(t):
    return furnace(max(0.0, t-3.0))

# (中心min, 宽度w, 损失mg)
PULSES = [(10,3.0,0.5),(33,3.5,3.0),(47,3.0,1.5),(56,3.0,1.2),(75,6.0,1.0)]
def true_mass(t):
    m = 10.0
    for c,w,a in PULSES:
        m -= a*0.5*(1+math.tanh((t-c)/w))
    return m
DRIFT = -0.0020  # mg/min, 示值漂移
def raw_mass(t): return true_mass(t) + DRIFT*t

TIMES = [2.0*i for i in range(51)]  # 0..100

def samples_block(noise=False, gap=False):
    rows = ["time_min,temp_c,mass_mg"]
    for t in TIMES:
        if gap and 60.0 <= t <= 66.0:
            continue  # 采样缺口: 54 -> 68, 不允许跨缺口插值
        m = raw_mass(t)
        if noise and t in (30.0, 78.0):
            m += (0.03 if t == 30.0 else 0.025)  # 非单调毛刺
        rows.append(f"{t:.1f},{sample_temp(t):.2f},{m:.4f}")
    return "\n".join(rows) + "\n"

def section(name, body):
    return f"@{name}\n{body}"

def drift_block():
    return ("method,ref_time_min\n"
            "blank_curve,0\n")

def blank_block():
    rows = ["time_min,mass_mg"]
    for t in (0,25,50,75,100):
        rows.append(f"{t},{DRIFT*t:.4f}")
    return "\n".join(rows) + "\n"

def program_block():
    return ("start_min,end_min,start_temp_c,end_temp_c\n"
            "0,20,30,200\n20,40,200,600\n40,70,600,900\n70,100,900,900\n")

def valves_block(switch=45):
    return ("time_min,gas,action\n"
            f"0,N2,open\n{switch},air,switch\n")

def initial_block(mass="10.000", basis="as_loaded", note="sample pan loaded mass"):
    return ("mass_mg,basis,note\n"
            f"{mass},{basis},{note}\n")

def write(fname, parts):
    with open(os.path.join(OUT, fname), "w", encoding="utf-8") as f:
        f.write("# tgasys offline bundle v1\n" + "\n".join(parts))

# 01 基线: 分层完整, 含漂移/空白/气氛/重叠峰
write("01-baseline.tga", [
    section("samples", samples_block()),
    section("drift_correction", drift_block()),
    section("furnace_program", program_block()),
    section("gas_valves", valves_block(45)),
    section("blank_curve", blank_block()),
    section("initial_mass", initial_block()),
])

# 03 仅改气体阀(局部依赖): 45 -> 48
write("03-valves-revised.tga", [section("gas_valves", valves_block(48))])

# 04 初重修订(局部依赖): 10.000 -> 10.120
write("04-initial-mass-revised.tga", [
    section("initial_mass", initial_block("10.120","revised","revised after buoyancy review"))])

# 05 采样新版本: 非单调噪声 + 采样缺口
write("05-samples-noise-gap.tga", [section("samples", samples_block(noise=True, gap=True))])

print("generated; final true mass t=100:", round(true_mass(100),4))

#!/usr/bin/env python3
"""
Interactive 3D Performance Visualization Script using Plotly

This script reads performance test results from a CSV file and generates
interactive 3D surface plots that can be rotated, zoomed, and explored in a web browser.

The plots show:
1. Throughput vs Processing Workers vs Submitting Workers
2. Failure Rate vs Processing Workers vs Submitting Workers

Usage:
    python3 plot_performance_interactive.py [csv_file]

If no CSV file is specified, it defaults to 'performance_results.csv'

Output:
    - performance_interactive.html - Combined interactive plot
    - throughput_interactive.html - Throughput plot only
    - failure_rate_interactive.html - Failure rate plot only
"""

import sys
import pandas as pd
import numpy as np
import plotly.graph_objects as go
from plotly.subplots import make_subplots
from pathlib import Path


def load_data(csv_path):
    """Load performance data from CSV file."""
    try:
        df = pd.read_csv(csv_path)
        print(f"Loaded {len(df)} data points from {csv_path}")
        return df
    except FileNotFoundError:
        print(f"Error: File '{csv_path}' not found.")
        sys.exit(1)
    except Exception as e:
        print(f"Error loading CSV: {e}")
        sys.exit(1)


def prepare_surface_data(df, z_column):
    """Prepare data for 3D surface plot."""
    # Get unique worker counts
    processing_workers = sorted(df['ProcessingWorkers'].unique())
    submitting_workers = sorted(df['SubmittingWorkers'].unique())
    
    # Create meshgrid
    X, Y = np.meshgrid(processing_workers, submitting_workers)
    
    # Create Z matrix
    Z = np.zeros_like(X, dtype=float)
    for i, pw in enumerate(processing_workers):
        for j, sw in enumerate(submitting_workers):
            value = df[(df['ProcessingWorkers'] == pw) & 
                      (df['SubmittingWorkers'] == sw)][z_column].values
            if len(value) > 0:
                Z[j, i] = value[0]
    
    return X, Y, Z


def create_interactive_surface(df, z_column, title, z_label, colorscale='Viridis'):
    """Create an interactive 3D surface plot."""
    X, Y, Z = prepare_surface_data(df, z_column)
    
    # Create surface plot
    fig = go.Figure(data=[go.Surface(
        x=X,
        y=Y,
        z=Z,
        colorscale=colorscale,
        colorbar=dict(title=z_label, len=0.7),
        hovertemplate='<b>Processing Workers</b>: %{x}<br>' +
                      '<b>Submitting Workers</b>: %{y}<br>' +
                      '<b>' + z_label + '</b>: %{z:.2f}<br>' +
                      '<extra></extra>'
    )])
    
    # Update layout
    fig.update_layout(
        title=dict(
            text=title,
            x=0.5,
            xanchor='center',
            font=dict(size=20)
        ),
        scene=dict(
            xaxis=dict(title='Processing Workers', backgroundcolor="rgb(230, 230,230)"),
            yaxis=dict(title='Submitting Workers', backgroundcolor="rgb(230, 230,230)"),
            zaxis=dict(title=z_label, backgroundcolor="rgb(230, 230,230)"),
            camera=dict(
                eye=dict(x=1.5, y=1.5, z=1.3)
            )
        ),
        width=1000,
        height=800,
        margin=dict(l=0, r=0, b=0, t=50)
    )
    
    return fig


def create_combined_interactive_plot(df):
    """Create a combined figure with both plots side by side."""
    # Prepare data for both surfaces
    X_t, Y_t, Z_throughput = prepare_surface_data(df, 'Throughput')
    X_f, Y_f, Z_failure = prepare_surface_data(df, 'FailureRate')
    
    # Create subplots
    fig = make_subplots(
        rows=1, cols=2,
        specs=[[{'type': 'surface'}, {'type': 'surface'}]],
        subplot_titles=('Transaction Throughput', 'Transaction Failure Rate'),
        horizontal_spacing=0.05
    )
    
    # Add throughput surface
    fig.add_trace(
        go.Surface(
            x=X_t,
            y=Y_t,
            z=Z_throughput,
            colorscale='Viridis',
            name='Throughput',
            colorbar=dict(
                title='Throughput<br>(tx/s)',
                x=0.45,
                len=0.7
            ),
            hovertemplate='<b>Processing Workers</b>: %{x}<br>' +
                          '<b>Submitting Workers</b>: %{y}<br>' +
                          '<b>Throughput</b>: %{z:.2f} tx/s<br>' +
                          '<extra></extra>'
        ),
        row=1, col=1
    )
    
    # Add failure rate surface
    fig.add_trace(
        go.Surface(
            x=X_f,
            y=Y_f,
            z=Z_failure,
            colorscale='Plasma',
            name='Failure Rate',
            colorbar=dict(
                title='Failure<br>Rate',
                x=1.0,
                len=0.7
            ),
            hovertemplate='<b>Processing Workers</b>: %{x}<br>' +
                          '<b>Submitting Workers</b>: %{y}<br>' +
                          '<b>Failure Rate</b>: %{z:.4f}<br>' +
                          '<extra></extra>'
        ),
        row=1, col=2
    )
    
    # Update layout
    fig.update_layout(
        title=dict(
            text='Performance Analysis: Worker Count Impact',
            x=0.5,
            xanchor='center',
            font=dict(size=22)
        ),
        width=1800,
        height=800,
        margin=dict(l=0, r=0, b=0, t=80)
    )
    
    # Update scene properties for both subplots
    for i in [1, 2]:
        fig.update_scenes(
            xaxis=dict(title='Processing Workers', backgroundcolor="rgb(230, 230,230)"),
            yaxis=dict(title='Submitting Workers', backgroundcolor="rgb(230, 230,230)"),
            camera=dict(eye=dict(x=1.5, y=1.5, z=1.3)),
            row=1, col=i
        )
    
    # Update z-axis titles
    fig.update_scenes(
        zaxis=dict(title='Throughput (tx/s)', backgroundcolor="rgb(230, 230,230)"),
        row=1, col=1
    )
    fig.update_scenes(
        zaxis=dict(title='Failure Rate', backgroundcolor="rgb(230, 230,230)"),
        row=1, col=2
    )
    
    return fig


def print_summary_statistics(df):
    """Print summary statistics from the data."""
    print("\n" + "="*70)
    print("SUMMARY STATISTICS")
    print("="*70)
    
    # Best throughput
    best_throughput_idx = df['Throughput'].idxmax()
    best_throughput = df.loc[best_throughput_idx]
    print(f"\nBest Throughput Configuration:")
    print(f"  Processing Workers: {int(best_throughput['ProcessingWorkers'])}")
    print(f"  Submitting Workers: {int(best_throughput['SubmittingWorkers'])}")
    print(f"  Throughput: {best_throughput['Throughput']:.2f} tx/s")
    print(f"  Failure Rate: {best_throughput['FailureRate']:.4f}")
    
    # Lowest failure rate
    lowest_failure_idx = df['FailureRate'].idxmin()
    lowest_failure = df.loc[lowest_failure_idx]
    print(f"\nLowest Failure Rate Configuration:")
    print(f"  Processing Workers: {int(lowest_failure['ProcessingWorkers'])}")
    print(f"  Submitting Workers: {int(lowest_failure['SubmittingWorkers'])}")
    print(f"  Throughput: {lowest_failure['Throughput']:.2f} tx/s")
    print(f"  Failure Rate: {lowest_failure['FailureRate']:.4f}")
    
    # Overall statistics
    print(f"\nOverall Statistics:")
    print(f"  Average Throughput: {df['Throughput'].mean():.2f} tx/s")
    print(f"  Std Dev Throughput: {df['Throughput'].std():.2f} tx/s")
    print(f"  Average Failure Rate: {df['FailureRate'].mean():.4f}")
    print(f"  Std Dev Failure Rate: {df['FailureRate'].std():.4f}")
    print("="*70 + "\n")


def main():
    """Main function."""
    # Get CSV file path from command line or use default
    if len(sys.argv) > 1:
        csv_path = sys.argv[1]
    else:
        csv_path = Path(__file__).parent / "performance_results.csv"
    
    print(f"Loading performance data from: {csv_path}")
    
    # Load data
    df = load_data(csv_path)
    
    # Print summary statistics
    print_summary_statistics(df)
    
    # Create output directory for plots
    output_dir = Path(__file__).parent
    
    # Create individual interactive plots
    print("\nGenerating interactive plots...")
    
    # Throughput plot
    fig_throughput = create_interactive_surface(
        df,
        'Throughput',
        'Transaction Throughput vs Worker Counts',
        'Throughput (tx/s)',
        'Viridis'
    )
    throughput_path = output_dir / 'throughput_interactive.html'
    fig_throughput.write_html(str(throughput_path))
    print(f"✓ Saved: {throughput_path}")
    
    # Failure rate plot
    fig_failure = create_interactive_surface(
        df,
        'FailureRate',
        'Transaction Failure Rate vs Worker Counts',
        'Failure Rate',
        'Plasma'
    )
    failure_path = output_dir / 'failure_rate_interactive.html'
    fig_failure.write_html(str(failure_path))
    print(f"✓ Saved: {failure_path}")
    
    # Combined plot
    fig_combined = create_combined_interactive_plot(df)
    combined_path = output_dir / 'performance_interactive.html'
    fig_combined.write_html(str(combined_path))
    print(f"✓ Saved: {combined_path}")
    
    print("\n" + "="*70)
    print("✓ All interactive plots generated successfully!")
    print("="*70)
    print(f"\nGenerated files:")
    print(f"  - {throughput_path}")
    print(f"  - {failure_path}")
    print(f"  - {combined_path}")
    print(f"\nOpen any HTML file in your web browser to interact with the plots.")
    print(f"You can:")
    print(f"  • Rotate: Click and drag")
    print(f"  • Zoom: Scroll or pinch")
    print(f"  • Pan: Right-click and drag")
    print(f"  • Reset: Double-click")
    print(f"  • Hover: See exact values")


if __name__ == "__main__":
    main()

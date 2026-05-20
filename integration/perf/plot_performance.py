#!/usr/bin/env python3
"""
3D Performance Visualization Script

This script reads performance test results from a CSV file and generates
3D surface plots showing:
1. Throughput vs Processing Workers vs Submitting Workers
2. Failure Rate vs Processing Workers vs Submitting Workers

Usage:
    python3 plot_performance.py [csv_file]

If no CSV file is specified, it defaults to 'performance_results.csv'
"""

import sys
import pandas as pd
import numpy as np
import matplotlib.pyplot as plt
from mpl_toolkits.mplot3d import Axes3D
from matplotlib import cm
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


def create_3d_surface_plot(df, z_column, title, z_label, output_file):
    """Create a 3D surface plot for the given data."""
    
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
    
    # Create figure
    fig = plt.figure(figsize=(12, 9))
    ax = fig.add_subplot(111, projection='3d')
    
    # Plot surface
    surf = ax.plot_surface(X, Y, Z, cmap=cm.viridis, 
                          linewidth=0, antialiased=True, alpha=0.8)
    
    # Add contour lines on the bottom
    ax.contour(X, Y, Z, zdir='z', offset=Z.min(), cmap=cm.viridis, alpha=0.3)
    
    # Labels and title
    ax.set_xlabel('Processing Workers', fontsize=12, labelpad=10)
    ax.set_ylabel('Submitting Workers', fontsize=12, labelpad=10)
    ax.set_zlabel(z_label, fontsize=12, labelpad=10)
    ax.set_title(title, fontsize=14, pad=20)
    
    # Add colorbar
    fig.colorbar(surf, ax=ax, shrink=0.5, aspect=5)
    
    # Adjust viewing angle
    ax.view_init(elev=25, azim=45)
    
    # Save figure
    plt.tight_layout()
    plt.savefig(output_file, dpi=300, bbox_inches='tight')
    print(f"Saved plot to: {output_file}")
    
    return fig


def create_combined_plot(df, output_file):
    """Create a figure with both plots side by side."""
    
    # Get unique worker counts
    processing_workers = sorted(df['ProcessingWorkers'].unique())
    submitting_workers = sorted(df['SubmittingWorkers'].unique())
    
    # Create meshgrid
    X, Y = np.meshgrid(processing_workers, submitting_workers)
    
    # Create Z matrices for both metrics
    Z_throughput = np.zeros_like(X, dtype=float)
    Z_failure = np.zeros_like(X, dtype=float)
    
    for i, pw in enumerate(processing_workers):
        for j, sw in enumerate(submitting_workers):
            mask = (df['ProcessingWorkers'] == pw) & (df['SubmittingWorkers'] == sw)
            throughput = df[mask]['Throughput'].values
            failure_rate = df[mask]['FailureRate'].values
            
            if len(throughput) > 0:
                Z_throughput[j, i] = throughput[0]
            if len(failure_rate) > 0:
                Z_failure[j, i] = failure_rate[0]
    
    # Create figure with two subplots
    fig = plt.figure(figsize=(20, 8))
    
    # Throughput plot
    ax1 = fig.add_subplot(121, projection='3d')
    surf1 = ax1.plot_surface(X, Y, Z_throughput, cmap=cm.viridis,
                            linewidth=0, antialiased=True, alpha=0.8)
    ax1.contour(X, Y, Z_throughput, zdir='z', offset=Z_throughput.min(), 
               cmap=cm.viridis, alpha=0.3)
    ax1.set_xlabel('Processing Workers', fontsize=11, labelpad=8)
    ax1.set_ylabel('Submitting Workers', fontsize=11, labelpad=8)
    ax1.set_zlabel('Throughput (tx/s)', fontsize=11, labelpad=8)
    ax1.set_title('Transaction Throughput', fontsize=13, pad=15)
    ax1.view_init(elev=25, azim=45)
    fig.colorbar(surf1, ax=ax1, shrink=0.5, aspect=5)
    
    # Failure rate plot
    ax2 = fig.add_subplot(122, projection='3d')
    surf2 = ax2.plot_surface(X, Y, Z_failure, cmap=cm.plasma,
                            linewidth=0, antialiased=True, alpha=0.8)
    ax2.contour(X, Y, Z_failure, zdir='z', offset=Z_failure.min(),
               cmap=cm.plasma, alpha=0.3)
    ax2.set_xlabel('Processing Workers', fontsize=11, labelpad=8)
    ax2.set_ylabel('Submitting Workers', fontsize=11, labelpad=8)
    ax2.set_zlabel('Failure Rate', fontsize=11, labelpad=8)
    ax2.set_title('Transaction Failure Rate', fontsize=13, pad=15)
    ax2.view_init(elev=25, azim=45)
    fig.colorbar(surf2, ax=ax2, shrink=0.5, aspect=5)
    
    # Overall title
    fig.suptitle('Performance Analysis: Worker Count Impact', 
                fontsize=16, y=0.98)
    
    # Save figure
    plt.tight_layout()
    plt.savefig(output_file, dpi=300, bbox_inches='tight')
    print(f"Saved combined plot to: {output_file}")
    
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
    
    # Create individual plots
    print("\nGenerating plots...")
    create_3d_surface_plot(
        df, 
        'Throughput',
        'Transaction Throughput vs Worker Counts',
        'Throughput (tx/s)',
        output_dir / 'throughput_3d.png'
    )
    
    create_3d_surface_plot(
        df,
        'FailureRate',
        'Transaction Failure Rate vs Worker Counts',
        'Failure Rate',
        output_dir / 'failure_rate_3d.png'
    )
    
    # Create combined plot
    create_combined_plot(
        df,
        output_dir / 'performance_combined_3d.png'
    )
    
    print("\n✓ All plots generated successfully!")
    print(f"\nGenerated files:")
    print(f"  - {output_dir / 'throughput_3d.png'}")
    print(f"  - {output_dir / 'failure_rate_3d.png'}")
    print(f"  - {output_dir / 'performance_combined_3d.png'}")


if __name__ == "__main__":
    main()
